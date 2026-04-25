package planner

import (
	"context"
	"errors"

	"local-agent/internal/agent/planner/candidate"
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
	Selector   candidate.Selector
	Mode       Mode
}

// Plan produces a compiled plan that the root agent Runtime still routes
// through ToolRouter/EffectInference/Policy/Approval/Executor.
func (p HybridPlanner) Plan(ctx context.Context, req Request) (compile.CompiledPlan, error) {
	normalizer := p.Normalizer
	classifier := p.Classifier
	fast := p.FastPath
	selector := p.Selector
	if selector == nil {
		selector = candidate.New()
	}
	normalized := normalizer.Normalize(req.UserMessage)
	classification := classifier.Classify(normalized)
	candidates, err := selector.Select(ctx, candidate.SelectionInput{Request: normalized, Catalog: p.Catalog, TopK: 8})
	if err != nil {
		return compile.CompiledPlan{}, err
	}
	classification = classificationFromCandidates(classification, candidates)

	if classification.NeedClarify {
		return p.compileValid(semantic.SemanticPlan{
			Decision:           semantic.SemanticPlanClarify,
			Goal:               normalized.Original,
			Confidence:         classification.Confidence,
			Domain:             string(classification.Domain),
			ClarifyingQuestion: clarificationQuestion(classification),
			Reason:             classification.Reason,
		}, normalized, candidates), nil
	}

	if p.Mode == "" {
		p.Mode = ModeHybrid
	}
	if p.Mode != ModeSemantic {
		if plan, ok := fast.Plan(fastpath.Input{ConversationID: req.ConversationID, Request: normalized, Classification: classification}); ok {
			return p.compileValid(plan, normalized, candidates), nil
		}
	}

	if classification.NeedTool && p.Mode != ModeHeuristic {
		plan, err := p.Semantic.Plan(ctx, normalized, classification, candidates)
		if err == nil {
			return p.compileValid(plan, normalized, candidates), nil
		}
		if !errors.Is(err, semantic.ErrUnavailable) {
			return compile.CompiledPlan{}, err
		}
		return p.compileValid(semantic.PlanFromCandidates(normalized, req.ConversationID, candidates), normalized, candidates), nil
	}
	if len(candidates) > 0 {
		return p.compileValid(semantic.PlanFromCandidates(normalized, req.ConversationID, candidates), normalized, candidates), nil
	}

	return p.compileValid(semantic.SemanticPlan{
		Decision:   semantic.SemanticPlanAnswer,
		Goal:       normalized.Original,
		Confidence: classification.Confidence,
		Language:   firstLanguage(normalized.LanguageHints),
		Domain:     string(classification.Domain),
		Reason:     "no deterministic tool match",
	}, normalized, candidates), nil
}

func classificationFromCandidates(cls intent.IntentClassification, candidates []candidate.ToolCandidate) intent.IntentClassification {
	if len(candidates) == 0 {
		return cls
	}
	cls.NeedTool = true
	if cls.Domain == intent.DomainChat && candidates[0].Card.Domain != "" {
		cls.Domain = intent.IntentDomain(candidates[0].Card.Domain)
	}
	if cls.Intent == "" || cls.Intent == "answer" {
		cls.Intent = "tool_request"
	}
	if cls.Confidence < 0.65 {
		cls.Confidence = 0.65
	}
	cls.Reason = "tool card candidates available"
	return cls
}

func (p HybridPlanner) compileValid(plan semantic.SemanticPlan, req normalize.NormalizedRequest, candidates []candidate.ToolCandidate) compile.CompiledPlan {
	validator := p.Validator
	validator.Options.Request = &req
	validator.Options.CandidateToolIDs = candidateIDs(candidates)
	validation := validator.Validate(plan)
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

func candidateIDs(candidates []candidate.ToolCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, item := range candidates {
		out = append(out, item.ToolID)
	}
	return out
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
