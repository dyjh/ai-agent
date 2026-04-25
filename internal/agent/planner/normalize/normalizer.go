package normalize

import (
	"strings"
	"unicode"
)

// NormalizedRequest is a structure-only view of a user request. It extracts
// stable slots and structural signals but does not choose tools or infer
// natural-language intent.
type NormalizedRequest struct {
	Original       string         `json:"original"`
	NormalizedText string         `json:"normalized_text"`
	LanguageHints  []string       `json:"language_hints,omitempty"`
	Workspace      string         `json:"workspace,omitempty"`
	QuotedTexts    []string       `json:"quoted_texts,omitempty"`
	PossibleFiles  []string       `json:"possible_files,omitempty"`
	URLs           []string       `json:"urls,omitempty"`
	HostID         string         `json:"host_id,omitempty"`
	KBID           string         `json:"kb_id,omitempty"`
	RunID          string         `json:"run_id,omitempty"`
	ApprovalID     string         `json:"approval_id,omitempty"`
	ExplicitToolID string         `json:"explicit_tool_id,omitempty"`
	Numbers        []string       `json:"numbers,omitempty"`
	DomainHints    []string       `json:"domain_hints,omitempty"`
	IntentHints    []string       `json:"intent_hints,omitempty"`
	Signals        []string       `json:"signals,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Normalizer extracts request features without making an executor decision.
type Normalizer struct{}

// New returns a default request normalizer.
func New() Normalizer {
	return Normalizer{}
}

// Normalize extracts normalized text, structural scope hints, ids, quoted
// spans, file candidates, URLs, numbers, and structural signals.
func (Normalizer) Normalize(message string) NormalizedRequest {
	original := strings.TrimSpace(message)
	normalizedText := strings.ToLower(original)
	quoted := ExtractQuotedTexts(original)
	workspace := ExtractWorkspace(original)
	req := NormalizedRequest{
		Original:       original,
		NormalizedText: normalizedText,
		LanguageHints:  languageHints(original),
		Workspace:      workspace,
		QuotedTexts:    quoted,
		URLs:           ExtractURLs(original),
		HostID:         ExtractID(original, []string{"host_id", "host", "主机"}),
		KBID:           ExtractID(original, []string{"kb_id", "kbid", "kb", "知识库"}),
		RunID:          ExtractID(original, []string{"run_id", "run"}),
		ApprovalID:     ExtractID(original, []string{"approval_id", "approval", "审批"}),
		ExplicitToolID: ExtractExplicitToolID(original),
		Numbers:        ExtractNumbers(original),
		Metadata:       map[string]any{},
	}
	req.PossibleFiles = PossibleWorkspaceFiles(workspace, quoted)
	req.Signals, req.DomainHints, req.IntentHints = signalsFor(req)
	if len(req.PossibleFiles) > 0 {
		req.Metadata["target"] = "file"
	}
	if workspace != "" {
		req.Metadata["workspace_present"] = true
	}
	return req
}

func languageHints(value string) []string {
	hasCJK := false
	hasLatin := false
	for _, r := range value {
		switch {
		case unicode.Is(unicode.Han, r):
			hasCJK = true
		case r <= unicode.MaxASCII && unicode.IsLetter(r):
			hasLatin = true
		}
	}
	out := []string{}
	if hasCJK {
		out = append(out, "zh")
	}
	if hasLatin {
		out = append(out, "en")
	}
	return out
}

func uniq(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
