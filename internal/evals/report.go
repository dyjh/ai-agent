package evals

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"local-agent/internal/security"
)

// BuildReport builds a redacted JSON/Markdown friendly report from a run.
func BuildReport(run EvalRun) (EvalReport, error) {
	report := EvalReport{
		RunID:      run.RunID,
		ByCategory: map[EvalCategory]CategoryStats{},
		CreatedAt:  time.Now().UTC(),
		Summary: EvalReportSummary{
			Total:  run.Total,
			Passed: run.Passed,
			Failed: run.Failed,
			Errors: run.Errors,
		},
	}
	if run.Total > 0 {
		report.Summary.PassRate = float64(run.Passed) / float64(run.Total)
	}
	for _, result := range run.Results {
		stats := report.ByCategory[result.Category]
		stats.Total++
		switch result.Status {
		case EvalRunPassed:
			stats.Passed++
		case EvalRunError:
			stats.Errors++
		default:
			stats.Failed++
		}
		report.ByCategory[result.Category] = stats
		report.Security.SecretFindings = append(report.Security.SecretFindings, result.SecretFindings...)
		for _, assertion := range result.Assertions {
			if assertion.Passed {
				continue
			}
			report.Failures = append(report.Failures, EvalReportFailure{
				CaseID:    result.CaseID,
				Category:  string(result.Category),
				Assertion: assertion.Name,
				Expected:  assertion.Expected,
				Actual:    assertion.Actual,
				Message:   security.RedactString(assertion.Message),
			})
			if assertion.Name == "forbidden_tools" {
				if tools, ok := assertion.Actual.([]string); ok {
					report.Security.ForbiddenToolCalls = append(report.Security.ForbiddenToolCalls, tools...)
				}
			}
		}
	}
	sort.Slice(report.Failures, func(i, j int) bool {
		if report.Failures[i].CaseID == report.Failures[j].CaseID {
			return report.Failures[i].Assertion < report.Failures[j].Assertion
		}
		return report.Failures[i].CaseID < report.Failures[j].CaseID
	})
	return report, nil
}

func (m *Manager) saveReport(report EvalReport) error {
	if err := m.EnsureLayout(); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(m.reportsRoot(), safeFileName(report.RunID)+".json"), report); err != nil {
		return err
	}
	md := RenderMarkdownReport(report)
	return os.WriteFile(filepath.Join(m.reportsRoot(), safeFileName(report.RunID)+".md"), []byte(security.RedactString(md)), 0o644)
}

func (m *Manager) reportPath(runID string) string {
	return filepath.Join(m.reportsRoot(), safeFileName(runID)+".json")
}

// RenderMarkdownReport renders a compact Markdown report.
func RenderMarkdownReport(report EvalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Eval Report %s\n\n", report.RunID)
	fmt.Fprintf(&b, "- total: %d\n", report.Summary.Total)
	fmt.Fprintf(&b, "- passed: %d\n", report.Summary.Passed)
	fmt.Fprintf(&b, "- failed: %d\n", report.Summary.Failed)
	fmt.Fprintf(&b, "- error: %d\n", report.Summary.Errors)
	fmt.Fprintf(&b, "- pass_rate: %.2f\n\n", report.Summary.PassRate)
	b.WriteString("## By Category\n\n")
	categories := make([]string, 0, len(report.ByCategory))
	for category := range report.ByCategory {
		categories = append(categories, string(category))
	}
	sort.Strings(categories)
	for _, name := range categories {
		stats := report.ByCategory[EvalCategory(name)]
		fmt.Fprintf(&b, "- %s: total=%d passed=%d failed=%d error=%d\n", name, stats.Total, stats.Passed, stats.Failed, stats.Errors)
	}
	b.WriteString("\n## Failures\n\n")
	if len(report.Failures) == 0 {
		b.WriteString("No failures.\n")
	} else {
		for _, failure := range report.Failures {
			fmt.Fprintf(&b, "- %s [%s] %s: %s\n", failure.CaseID, failure.Category, failure.Assertion, security.RedactString(failure.Message))
		}
	}
	b.WriteString("\n## Security\n\n")
	fmt.Fprintf(&b, "- secret_findings: %d\n", len(report.Security.SecretFindings))
	fmt.Fprintf(&b, "- forbidden_tool_calls: %d\n", len(report.Security.ForbiddenToolCalls))
	fmt.Fprintf(&b, "- unexpected_auto_approvals: %d\n", len(report.Security.UnexpectedAutoApprovals))
	return security.RedactString(b.String())
}
