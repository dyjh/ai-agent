package planner

import (
	"context"
	"errors"
	"strings"

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
	ModeHeuristic      Mode = "heuristic"
	ModeSemantic       Mode = "semantic"
	ModeHybrid         Mode = "hybrid"
	ModeSemanticStrict Mode = "semantic_strict"
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
	Config     semantic.Config
}

// Plan produces a compiled plan that the root agent Runtime still routes
// through ToolRouter/EffectInference/Policy/Approval/Executor.
func (p HybridPlanner) Plan(ctx context.Context, req Request) (compile.CompiledPlan, error) {
	cfg := p.effectiveConfig()
	normalizer := p.Normalizer
	classifier := p.Classifier
	fast := p.FastPath
	selector := p.Selector
	if selector == nil {
		selector = candidate.New()
	}
	normalized := normalizer.Normalize(req.UserMessage)
	classification := classifier.Classify(normalized)

	if cfg.ChatGate.Enabled {
		switch gate := chatGate(classification, normalized); gate {
		case chatGateDirectAnswer:
			return p.compileValid(semantic.SemanticPlan{
				Decision:      semantic.SemanticPlanAnswer,
				Goal:          normalized.Original,
				Confidence:    classification.Confidence,
				Language:      firstLanguage(normalized.LanguageHints),
				Domain:        string(classification.Domain),
				PlannerSource: semantic.PlannerSourceNoToolAnswer,
				Answer:        "",
				Reason:        "planner_source=no_tool_answer; chat gate classified request as direct answer",
			}, normalized, nil, cfg), nil
		case chatGateClarify:
			return p.compileValid(semantic.SemanticPlan{
				Decision:           semantic.SemanticPlanClarify,
				Goal:               normalized.Original,
				Confidence:         classification.Confidence,
				Domain:             string(classification.Domain),
				PlannerSource:      semantic.PlannerSourceClarify,
				ClarifyingQuestion: "请补充要操作的目标和范围。",
				Reason:             "planner_source=clarify; chat gate requires more detail",
			}, normalized, nil, cfg), nil
		}
	}

	candidates, err := p.selectCandidates(ctx, selector, normalized, cfg)
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
			PlannerSource:      semantic.PlannerSourceClarify,
			ClarifyingQuestion: clarificationQuestion(classification),
			Reason:             "planner_source=clarify; " + classification.Reason,
		}, normalized, candidates, cfg), nil
	}

	if normalized.ExplicitToolID != "" {
		return p.compileValid(withSource(semantic.PlanFromCandidates(normalized, req.ConversationID, explicitCandidates(normalized.ExplicitToolID, candidates)), semantic.PlannerSourceExplicitTool), normalized, candidates, cfg), nil
	}

	mode := Mode(cfg.Mode)
	if cfg.ToolPlanner.EnableFastPath && !cfg.ToolPlanner.RequireLLMForToolChoice && mode != ModeSemantic && mode != ModeSemanticStrict {
		if plan, ok := fast.Plan(fastpath.Input{ConversationID: req.ConversationID, Request: normalized, Classification: classification}); ok {
			return p.compileValid(withSource(plan, semantic.PlannerSourceFastPath), normalized, candidates, cfg), nil
		}
	}

	if shouldUseToolPlanner(classification, candidates, mode) {
		plan, err := p.Semantic.Plan(ctx, normalized, classification, candidates)
		if err == nil {
			return p.compileValid(withSource(plan, semantic.PlannerSourceSemanticLLM), normalized, candidates, cfg), nil
		}
		if !errors.Is(err, semantic.ErrUnavailable) {
			return compile.CompiledPlan{}, err
		}
		if cfg.ToolPlanner.AllowCandidateFallback && !cfg.ToolPlanner.RequireLLMForToolChoice && mode != ModeSemanticStrict {
			return p.compileValid(withSource(semantic.PlanFromCandidates(normalized, req.ConversationID, candidates), semantic.PlannerSourceCandidateFallback), normalized, candidates, cfg), nil
		}
		return p.compileValid(semantic.SemanticPlan{
			Decision:           semantic.SemanticPlanClarify,
			Goal:               normalized.Original,
			Confidence:         classification.Confidence,
			Domain:             string(classification.Domain),
			PlannerSource:      semantic.PlannerSourceToolUnavailable,
			ClarifyingQuestion: "语义工具规划模型当前不可用，无法安全选择工具。请稍后重试，或明确指定工具。",
			Reason:             "planner_source=tool_planner_unavailable; semantic planner unavailable and candidate fallback disabled",
		}, normalized, candidates, cfg), nil
	}
	if mode != ModeSemanticStrict && cfg.ToolPlanner.AllowCandidateFallback && !cfg.ToolPlanner.RequireLLMForToolChoice && len(candidates) > 0 {
		return p.compileValid(withSource(semantic.PlanFromCandidates(normalized, req.ConversationID, candidates), semantic.PlannerSourceCandidateFallback), normalized, candidates, cfg), nil
	}

	return p.compileValid(semantic.SemanticPlan{
		Decision:      semantic.SemanticPlanAnswer,
		Goal:          normalized.Original,
		Confidence:    classification.Confidence,
		Language:      firstLanguage(normalized.LanguageHints),
		Domain:        string(classification.Domain),
		PlannerSource: semantic.PlannerSourceNoToolAnswer,
		Reason:        "planner_source=no_tool_answer; no tool plan selected",
	}, normalized, candidates, cfg), nil
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

func (p HybridPlanner) compileValid(plan semantic.SemanticPlan, req normalize.NormalizedRequest, candidates []candidate.ToolCandidate, cfg semantic.Config) compile.CompiledPlan {
	validator := p.Validator
	validator.Options.Request = &req
	validator.Options.CandidateToolIDs = candidateIDs(candidates)
	validator.Options.AllowCrossCandidate = cfg.ToolPlanner.AllowCrossCandidate
	validator.Options.RequireCandidateMatch = !cfg.ToolPlanner.AllowCrossCandidate && (semantic.IsSemanticStrictMode(cfg.Mode) || len(candidates) > 0)
	validator.Options.RequireSemanticSource = cfg.ToolPlanner.RequireLLMForToolChoice || semantic.IsSemanticStrictMode(cfg.Mode)
	validation := validator.Validate(plan)
	if !validation.Valid {
		question := validation.Clarify
		if question == "" {
			question = "这个请求还缺少必要参数或包含不安全的工具选择，请补充目标、范围和参数。"
		}
		compiled := p.Compiler.Compile(semantic.SemanticPlan{
			Decision:           semantic.SemanticPlanClarify,
			Goal:               plan.Goal,
			Confidence:         plan.Confidence,
			Domain:             plan.Domain,
			PlannerSource:      plan.PlannerSource,
			ClarifyingQuestion: question,
			Reason:             reasonWithSource(plan.PlannerSource, joinErrors(validation.Errors)),
		})
		compiled.CandidateCount = len(candidates)
		return compiled
	}
	compiled := p.Compiler.Compile(*validation.Sanitized)
	compiled.CandidateCount = len(candidates)
	if compiled.PlannerSource == "" {
		compiled.PlannerSource = plan.PlannerSource
	}
	return compiled
}

func (p HybridPlanner) effectiveConfig() semantic.Config {
	cfg := p.Config
	if p.Mode != "" {
		cfg.Mode = string(p.Mode)
	}
	if cfg.Mode == "" {
		cfg.Mode = string(ModeHybrid)
	}
	return semantic.NormalizeConfig(cfg)
}

func (p HybridPlanner) selectCandidates(ctx context.Context, selector candidate.Selector, req normalize.NormalizedRequest, cfg semantic.Config) ([]candidate.ToolCandidate, error) {
	if cfg.ToolPlanner.CandidateSelectorAsContext || !semantic.IsSemanticStrictMode(cfg.Mode) {
		return selector.Select(ctx, candidate.SelectionInput{Request: req, Catalog: p.Catalog, TopK: 8})
	}
	items := p.Catalog.All()
	out := make([]candidate.ToolCandidate, 0, len(items))
	for _, spec := range items {
		out = append(out, candidate.ToolCandidate{
			ToolID: spec.ToolID,
			Score:  0,
			Reason: "full catalog context",
			Card: catalog.ToolCard{
				ToolID:           spec.ToolID,
				Domain:           spec.Domain,
				Title:            spec.Title,
				Description:      spec.Description,
				DescriptionZH:    spec.DescriptionZH,
				WhenToUse:        append([]string(nil), spec.WhenToUse...),
				WhenNotToUse:     append([]string(nil), spec.WhenNotToUse...),
				RequiredSlots:    append([]string(nil), spec.RequiredSlots...),
				InputSchema:      cloneMap(spec.InputSchema),
				Defaults:         cloneMap(spec.Defaults),
				Effects:          append([]string(nil), spec.DefaultEffects...),
				RiskLevel:        spec.RiskLevel,
				AutoSelectable:   spec.AutoSelectable,
				Examples:         append([]catalog.ToolExample(nil), spec.Examples...),
				NegativeExamples: append([]catalog.ToolExample(nil), spec.NegativeExamples...),
			},
		})
	}
	return out, nil
}

func shouldUseToolPlanner(cls intent.IntentClassification, candidates []candidate.ToolCandidate, mode Mode) bool {
	if mode == ModeHeuristic {
		return false
	}
	return cls.NeedTool || len(candidates) > 0 || mode == ModeSemantic || mode == ModeSemanticStrict
}

func withSource(plan semantic.SemanticPlan, source semantic.PlannerSource) semantic.SemanticPlan {
	plan.PlannerSource = source
	plan.Reason = reasonWithSource(source, plan.Reason)
	return plan
}

func reasonWithSource(source semantic.PlannerSource, reason string) string {
	prefix := "planner_source=" + string(source)
	if strings.TrimSpace(reason) == "" {
		return prefix
	}
	if strings.Contains(reason, "planner_source=") {
		return reason
	}
	return prefix + "; " + reason
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func candidateIDs(candidates []candidate.ToolCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, item := range candidates {
		out = append(out, item.ToolID)
	}
	return out
}

func explicitCandidates(toolID string, candidates []candidate.ToolCandidate) []candidate.ToolCandidate {
	out := make([]candidate.ToolCandidate, 0, 1)
	for _, item := range candidates {
		if item.ToolID == toolID {
			out = append(out, item)
			break
		}
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
