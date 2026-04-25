package planner

import (
	"context"
	"errors"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/compile"
	"local-agent/internal/agent/planner/fastpath"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/agent/planner/validate"
)

// Mode selects deterministic, semantic, or hybrid planning.
type Mode string

const (
	ModeHeuristic Mode = "heuristic"
	ModeSemantic  Mode = "semantic"
	ModeHybrid    Mode = "hybrid"
)

// HybridPlanner coordinates normalizer, classifier, fast path, semantic
// planner, validator, and compiler. It does not execute tools.
type HybridPlanner struct {
	Normalizer normalize.Normalizer
	Classifier intent.Classifier
	FastPath   fastpath.DeterministicFastPath
	Semantic   semantic.LLMSemanticPlanner
	Validator  validate.Validator
	Compiler   compile.Compiler
	Catalog    catalog.PlanningCatalog
	Mode       Mode
}

// Plan produces a compiled plan that the root agent Runtime still routes
// through ToolRouter/EffectInference/Policy/Approval/Executor.
func (p HybridPlanner) Plan(ctx context.Context, req Request) (compile.CompiledPlan, error) {
	normalizer := p.Normalizer
	classifier := p.Classifier
	fast := p.FastPath
	normalized := normalizer.Normalize(req.UserMessage)
	classification := classifier.Classify(normalized)

	if classification.NeedClarify {
		return p.compileValid(semantic.SemanticPlan{
			Decision:           semantic.SemanticPlanClarify,
			Goal:               normalized.Original,
			Confidence:         classification.Confidence,
			Domain:             string(classification.Domain),
			ClarifyingQuestion: clarificationQuestion(classification),
			Reason:             classification.Reason,
		}), nil
	}

	if p.Mode == "" {
		p.Mode = ModeHybrid
	}
	if p.Mode != ModeSemantic {
		if plan, ok := fast.Plan(fastpath.Input{ConversationID: req.ConversationID, Request: normalized, Classification: classification}); ok {
			return p.compileValid(plan), nil
		}
	}

	if classification.NeedTool && p.Mode != ModeHeuristic {
		plan, err := p.Semantic.Plan(ctx, normalized, classification, p.Catalog)
		if err == nil {
			return p.compileValid(plan), nil
		}
		if !errors.Is(err, semantic.ErrUnavailable) {
			return compile.CompiledPlan{}, err
		}
		if p.Mode == ModeSemantic {
			return p.compileValid(semantic.SemanticPlan{
				Decision:           semantic.SemanticPlanClarify,
				Goal:               normalized.Original,
				Confidence:         0.4,
				Domain:             string(classification.Domain),
				ClarifyingQuestion: "当前语义规划器不可用，请更明确地说明要使用的工具、范围和参数。",
				Reason:             "semantic planner unavailable",
			}), nil
		}
	}

	return p.compileValid(semantic.SemanticPlan{
		Decision:   semantic.SemanticPlanAnswer,
		Goal:       normalized.Original,
		Confidence: classification.Confidence,
		Language:   firstLanguage(normalized.LanguageHints),
		Domain:     string(classification.Domain),
		Reason:     "no deterministic tool match",
	}), nil
}

func (p HybridPlanner) compileValid(plan semantic.SemanticPlan) compile.CompiledPlan {
	validation := p.Validator.Validate(plan)
	if !validation.Valid {
		question := validation.Clarify
		if question == "" {
			question = "这个请求还缺少必要参数或包含不安全的工具选择，请补充目标、范围和参数。"
		}
		return p.Compiler.Compile(semantic.SemanticPlan{
			Decision:           semantic.SemanticPlanClarify,
			Goal:               plan.Goal,
			Confidence:         plan.Confidence,
			Domain:             plan.Domain,
			ClarifyingQuestion: question,
			Reason:             joinErrors(validation.Errors),
		})
	}
	return p.Compiler.Compile(*validation.Sanitized)
}

func clarificationQuestion(cls intent.IntentClassification) string {
	if cls.Reason != "" {
		return "请补充范围或参数：" + cls.Reason
	}
	return "请补充要操作的目标和范围。"
}

func firstLanguage(hints []string) string {
	if len(hints) == 0 {
		return ""
	}
	return hints[0]
}

func joinErrors(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += "; " + item
	}
	return out
}
