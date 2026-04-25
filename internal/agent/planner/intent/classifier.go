package intent

import (
	"strings"

	"local-agent/internal/agent/planner/normalize"
)

// Classifier maps normalized signals into coarse intent labels.
type Classifier struct{}

// New returns a default classifier.
func New() Classifier {
	return Classifier{}
}

// Classify chooses a domain/intent pair but never selects a concrete executor.
func (Classifier) Classify(req normalize.NormalizedRequest) IntentClassification {
	signals := signalSet(req.Signals)
	out := IntentClassification{
		Domain:     DomainChat,
		Intent:     "answer",
		Confidence: 0.4,
		Signals:    append([]string(nil), req.Signals...),
		Reason:     "no tool intent signal",
	}

	switch {
	case signals["system_overview"]:
		return tool(DomainOps, "system_overview", 0.95, req.Signals, "normalized system overview signal")
	case signals["disk_usage"]:
		return tool(DomainOps, "disk_usage", 0.93, req.Signals, "normalized disk usage signal")
	case signals["memory_usage"]:
		return tool(DomainOps, "memory_usage", 0.92, req.Signals, "normalized memory usage signal")
	case signals["processes"]:
		return tool(DomainOps, "processes", 0.9, req.Signals, "normalized process signal")
	case signals["logs"] && !signals["docker_ops"] && !signals["k8s_ops"] && !signals["ssh_ops"] && !signals["local_restart"]:
		return IntentClassification{
			Domain:      DomainOps,
			Intent:      "logs",
			Confidence:  0.55,
			Signals:     append([]string(nil), req.Signals...),
			NeedTool:    true,
			NeedClarify: true,
			Reason:      "logs requested without local/docker/k8s/ssh scope",
		}
	case signals["docker_ops"]:
		return tool(DomainOps, "docker", 0.9, req.Signals, "docker ops signal")
	case signals["k8s_ops"]:
		return tool(DomainOps, "k8s", 0.9, req.Signals, "kubernetes ops signal")
	case signals["ssh_ops"]:
		return tool(DomainOps, "ssh", 0.88, req.Signals, "ssh ops signal")
	case signals["local_restart"]:
		return tool(DomainOps, "service_restart", 0.92, req.Signals, "local service restart signal")
	case signals["runbook_ops"]:
		return tool(DomainOps, "runbook", 0.86, req.Signals, "runbook ops signal")
	case signals["fix_tests"]:
		return tool(DomainCode, "fix_tests", 0.94, req.Signals, "test repair signal")
	case signals["run_tests"]:
		return tool(DomainCode, "run_tests", 0.93, req.Signals, "run tests signal")
	case signals["read_file"] && len(req.PossibleFiles) > 0:
		return tool(DomainCode, "read_file", 0.96, req.Signals, "workspace file read signal")
	case signals["read_file"]:
		return clarify(DomainCode, "read_file", req.Signals, "file read requested without a concrete file")
	case signals["search_text"] && firstQuoted(req) != "":
		return tool(DomainCode, "search_text", 0.96, req.Signals, "quoted search text signal")
	case signals["search_text"]:
		return clarify(DomainCode, "search_text", req.Signals, "text search requested without query")
	case signals["patch_apply"]:
		return tool(DomainCode, "patch_apply", 0.88, req.Signals, "patch apply signal")
	case signals["patch_validate"]:
		return tool(DomainCode, "patch_validate", 0.88, req.Signals, "patch validation signal")
	case signals["install_dependency"]:
		return tool(DomainCode, "install_dependency", 0.88, req.Signals, "dependency install signal")
	case signals["inspect_project"]:
		return tool(DomainCode, "inspect_project", 0.82, req.Signals, "generic code project signal")
	case signals["git_status"]:
		return tool(DomainGit, "status", 0.95, req.Signals, "git status signal")
	case signals["git_diff"]:
		return tool(DomainGit, "diff", 0.95, req.Signals, "git diff signal")
	case signals["git_diff_summary"]:
		return tool(DomainGit, "diff_summary", 0.95, req.Signals, "git diff summary signal")
	case signals["git_commit_message"]:
		return tool(DomainGit, "commit_message", 0.9, req.Signals, "git commit message signal")
	case signals["git_log"]:
		return tool(DomainGit, "log", 0.9, req.Signals, "git log signal")
	case signals["kb_answer"]:
		return tool(DomainRAG, "answer", 0.92, req.Signals, "knowledge answer signal")
	case signals["kb_retrieve"]:
		return tool(DomainRAG, "retrieve", 0.9, req.Signals, "knowledge retrieval signal")
	case signals["memory_extract"]:
		return tool(DomainMemory, "extract_candidates", 0.92, req.Signals, "memory extraction signal")
	case signals["memory_archive"]:
		return tool(DomainMemory, "archive", 0.9, req.Signals, "memory archive signal")
	}

	if strings.TrimSpace(req.Original) == "" {
		out.Reason = "empty request"
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

func signalSet(signals []string) map[string]bool {
	set := map[string]bool{}
	for _, signal := range signals {
		set[strings.TrimSpace(signal)] = true
	}
	return set
}

func firstQuoted(req normalize.NormalizedRequest) string {
	for _, item := range req.QuotedTexts {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}
