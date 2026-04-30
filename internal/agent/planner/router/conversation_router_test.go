package router

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

func TestLightweightConversationRouterRoutesGeneralKnowledgeToDirectAnswer(t *testing.T) {
	normalizer := normalize.New()
	req := normalizer.Normalize("神经内科分为哪些大类")
	decision, err := (LightweightConversationRouter{}).Route(context.Background(), ConversationRouteInput{
		UserMessage:    req.Original,
		Normalized:     req,
		Classification: intent.New().Classify(req),
	})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if decision.Route != RouteDirectAnswer {
		t.Fatalf("route = %s, want direct_answer", decision.Route)
	}
	if decision.Source != RouteSourceLightweight {
		t.Fatalf("source = %s, want lightweight", decision.Source)
	}
}

func TestLightweightConversationRouterRoutesWorkspaceToToolNeeded(t *testing.T) {
	normalizer := normalize.New()
	req := normalizer.Normalize("请读取 workspace: /tmp/project 中的 `README.md`")
	decision, err := (LightweightConversationRouter{}).Route(context.Background(), ConversationRouteInput{
		UserMessage:    req.Original,
		Normalized:     req,
		Classification: intent.New().Classify(req),
	})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if decision.Route != RouteToolNeeded {
		t.Fatalf("route = %s, want tool_needed", decision.Route)
	}
	if decision.Source != RouteSourceLightweight {
		t.Fatalf("source = %s, want lightweight", decision.Source)
	}
}

func TestLLMConversationRouterUsesStrictRoutePrompt(t *testing.T) {
	model := &routeModel{response: `{"route":"direct_answer","confidence":0.91,"reason":"general knowledge"}`}
	normalizer := normalize.New()
	req := normalizer.Normalize("解释一下 Go interface")
	decision, err := (LLMConversationRouter{
		Model:       model,
		MaxRetries:  1,
		RequireJSON: true,
		Fallback:    LightweightConversationRouter{},
	}).Route(context.Background(), ConversationRouteInput{
		UserMessage:    req.Original,
		Normalized:     req,
		Classification: intent.New().Classify(req),
	})
	if err != nil {
		t.Fatalf("Route error = %v", err)
	}
	if decision.Route != RouteDirectAnswer {
		t.Fatalf("route = %s, want direct_answer", decision.Route)
	}
	if decision.Source != RouteSourceLLM {
		t.Fatalf("source = %s, want llm", decision.Source)
	}
	if !strings.Contains(model.prompt, "Do not answer the user's question.") || !strings.Contains(model.prompt, "Do not choose a specific tool.") {
		t.Fatalf("prompt missing router safety rules:\n%s", model.prompt)
	}
}

type routeModel struct {
	response string
	prompt   string
}

func (m *routeModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.prompt = messages[len(messages)-1].Content
	return &schema.Message{Role: schema.Assistant, Content: m.response}, nil
}
