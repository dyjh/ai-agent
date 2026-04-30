package validate

import (
	"fmt"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/core"
)

// PlanValidationResult is the local validation result for a SemanticPlan.
type PlanValidationResult struct {
	Valid     bool                   `json:"valid"`
	Errors    []string               `json:"errors,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
	Clarify   string                 `json:"clarify,omitempty"`
	Sanitized *semantic.SemanticPlan `json:"sanitized,omitempty"`
}

// Options configures local safety checks.
type Options struct {
	SensitivePaths        []string
	Request               *normalize.NormalizedRequest
	CandidateToolIDs      []string
	AllowCrossCandidate   bool
	RequireCandidateMatch bool
	RequireSemanticSource bool
}

// Validator validates SemanticPlan without executing anything.
type Validator struct {
	Catalog catalog.PlanningCatalog
	Options Options
}

// New creates a plan validator.
func New(cat catalog.PlanningCatalog, options Options) Validator {
	return Validator{Catalog: cat, Options: options}
}

// Validate validates and sanitizes a SemanticPlan.
func (v Validator) Validate(plan semantic.SemanticPlan) PlanValidationResult {
	result := PlanValidationResult{}
	sanitized := clonePlan(plan)
	if sanitized.Confidence < 0 {
		sanitized.Confidence = 0
	}
	if sanitized.Confidence > 1 {
		sanitized.Confidence = 1
	}

	switch sanitized.Decision {
	case semantic.SemanticPlanAnswer:
		if sanitized.PlannerSource == semantic.PlannerSourceSemanticLLM {
			result.Errors = append(result.Errors, "semantic tool planner must not produce final answers")
			result.Clarify = "这个请求不能由工具规划器直接生成最终回答；请改走普通回答流程，或补充需要操作的本地资源和工具范围。"
			return result
		}
		result.Valid = true
		result.Sanitized = &sanitized
		return result
	case semantic.SemanticPlanNoTool:
		sanitized.Answer = ""
		sanitized.Steps = nil
		result.Valid = true
		result.Sanitized = &sanitized
		return result
	case semantic.SemanticPlanCapabilityLimitation:
		sanitized.Answer = ""
		sanitized.Steps = nil
		if sanitized.CapabilityMessage == "" {
			sanitized.CapabilityMessage = "当前可用工具不足，无法安全完成这个操作。"
		}
		result.Valid = true
		result.Sanitized = &sanitized
		return result
	case semantic.SemanticPlanClarify:
		if sanitized.ClarifyingQuestion == "" {
			sanitized.ClarifyingQuestion = "需要补充哪些目标或范围？"
		}
		result.Valid = true
		result.Sanitized = &sanitized
		return result
	case semantic.SemanticPlanTool, semantic.SemanticPlanMultiStep:
		if v.Options.RequireSemanticSource && sanitized.PlannerSource != semantic.PlannerSourceSemanticLLM && sanitized.PlannerSource != semantic.PlannerSourceExplicitTool {
			result.Errors = append(result.Errors, "semantic_strict tool plan must come from semantic_llm or explicit_tool_request")
			result.Clarify = "语义工具规划模型当前不可用，无法安全选择工具。请稍后重试，或明确指定工具。"
			return result
		}
	default:
		result.Errors = append(result.Errors, fmt.Sprintf("invalid decision %q", sanitized.Decision))
		return result
	}

	if len(sanitized.Steps) == 0 {
		result.Clarify = "需要明确要调用的工具和必要参数。"
		result.Errors = append(result.Errors, "tool plan has no steps")
		return result
	}
	for idx := range sanitized.Steps {
		step := &sanitized.Steps[idx]
		step.Input = core.CloneMap(step.Input)
		stepResult := v.validateStep(step)
		result.Errors = append(result.Errors, stepResult.Errors...)
		result.Warnings = append(result.Warnings, stepResult.Warnings...)
		if stepResult.Clarify != "" && result.Clarify == "" {
			result.Clarify = stepResult.Clarify
		}
	}
	if len(result.Errors) > 0 {
		return result
	}
	result.Valid = true
	result.Sanitized = &sanitized
	return result
}

type stepValidation struct {
	Errors   []string
	Warnings []string
	Clarify  string
}

func clonePlan(plan semantic.SemanticPlan) semantic.SemanticPlan {
	cp := plan
	cp.Steps = make([]semantic.SemanticPlanStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		step.Input = core.CloneMap(step.Input)
		step.DependsOn = append([]int(nil), step.DependsOn...)
		cp.Steps = append(cp.Steps, step)
	}
	return cp
}
