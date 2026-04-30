package semantic

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"local-agent/internal/agent/planner/candidate"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

var ErrUnavailable = errors.New("semantic planner unavailable")

// ChatModel is the Eino chat surface needed by the semantic planner.
type ChatModel interface {
	Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

// Config controls LLM semantic planning. The planner is inert unless enabled.
type Config struct {
	Mode                    string                   `json:"mode" yaml:"mode"`
	SemanticEnabled         bool                     `json:"semantic_enabled" yaml:"semantic_enabled"`
	SemanticShadowMode      bool                     `json:"semantic_shadow_mode" yaml:"semantic_shadow_mode"`
	MaxRetries              int                      `json:"max_retries" yaml:"max_retries"`
	RequireSchemaValidation bool                     `json:"require_schema_validation" yaml:"require_schema_validation"`
	ConversationRouter      ConversationRouterConfig `json:"conversation_router" yaml:"conversation_router"`
	ChatGate                ChatGateConfig           `json:"chat_gate" yaml:"chat_gate"`
	ToolPlanner             ToolPlannerConfig        `json:"tool_planner" yaml:"tool_planner"`
	Shell                   ShellPlannerConfig       `json:"shell" yaml:"shell"`
	Debug                   DebugConfig              `json:"debug" yaml:"debug"`
}

// LLMSemanticPlanner asks a chat model for a SemanticPlan JSON object only.
type LLMSemanticPlanner struct {
	Model  ChatModel
	Config Config
}

// NewLLMPlanner creates a semantic planner behind a config gate.
func NewLLMPlanner(model ChatModel, cfg Config) LLMSemanticPlanner {
	cfg = NormalizeConfig(cfg)
	return LLMSemanticPlanner{Model: model, Config: cfg}
}

// Plan returns a structured SemanticPlan from candidate tools. It never executes tools.
func (p LLMSemanticPlanner) Plan(ctx context.Context, req normalize.NormalizedRequest, cls intent.IntentClassification, candidates []candidate.ToolCandidate) (SemanticPlan, error) {
	if !p.Config.SemanticEnabled || p.Model == nil {
		return SemanticPlan{}, ErrUnavailable
	}
	prompt := BuildPrompt(req, cls, candidates)
	var last string
	for attempt := 0; attempt <= p.Config.MaxRetries; attempt++ {
		content := prompt
		if attempt > 0 {
			content = RepairPrompt(last)
		}
		msg, err := p.Model.Generate(ctx, []*schema.Message{
			{Role: schema.System, Content: "You are a safe planner. Output only SemanticPlan JSON. Never execute tools."},
			{Role: schema.User, Content: content},
		})
		if err != nil {
			return SemanticPlan{}, err
		}
		last = strings.TrimSpace(msg.Content)
		plan, err := ParsePlan(last)
		if err == nil {
			return plan, nil
		}
	}
	return SemanticPlan{}, ErrUnavailable
}
