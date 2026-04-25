package intent

import (
	"strings"

	"local-agent/internal/agent/planner/normalize"
)

// Classifier maps structural slots into coarse planning labels. It does not
// perform natural-language tool intent routing.
type Classifier struct{}

// New returns a default classifier.
func New() Classifier {
	return Classifier{}
}

// Classify chooses a coarse domain/intent pair but never selects an executor.
func (Classifier) Classify(req normalize.NormalizedRequest) IntentClassification {
	out := IntentClassification{
		Domain:     DomainChat,
		Intent:     "answer",
		Confidence: 0.4,
		Signals:    append([]string(nil), req.Signals...),
		Reason:     "no structural tool slot",
	}

	switch {
	case strings.TrimSpace(req.Original) == "":
		out.Confidence = 0.9
		out.Reason = "empty request"
		return out
	case req.ExplicitToolID != "":
		return tool(DomainChat, "explicit_tool", 0.95, req.Signals, "explicit tool id slot")
	case req.ApprovalID != "":
		return tool(DomainApproval, "approval", 0.85, req.Signals, "approval id slot")
	case req.RunID != "":
		return tool(DomainRun, "run", 0.85, req.Signals, "run id slot")
	case req.KBID != "":
		return tool(DomainRAG, "knowledge", 0.82, req.Signals, "kb id slot")
	case req.HostID != "":
		return tool(DomainOps, "host_scoped", 0.82, req.Signals, "host id slot")
	case len(req.PossibleFiles) > 0 || req.Workspace != "":
		return tool(DomainCode, "workspace_scoped", 0.78, req.Signals, "workspace or file slot")
	case len(req.URLs) > 0:
		return tool(DomainChat, "url_scoped", 0.65, req.Signals, "url slot")
	}

	return out
}

func tool(domain IntentDomain, intent string, confidence float64, signals []string, reason string) IntentClassification {
	return IntentClassification{
		Domain:     domain,
		Intent:     intent,
		Confidence: confidence,
		Signals:    append([]string(nil), signals...),
		NeedTool:   true,
		Reason:     reason,
	}
}

func clarify(domain IntentDomain, intent string, signals []string, reason string) IntentClassification {
	return IntentClassification{
		Domain:      domain,
		Intent:      intent,
		Confidence:  0.55,
		Signals:     append([]string(nil), signals...),
		NeedTool:    true,
		NeedClarify: true,
		Reason:      reason,
	}
}
