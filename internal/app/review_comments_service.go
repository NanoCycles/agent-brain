package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

type ReviewCommentReport struct {
	Decision     domain.ReviewDecision `json:"decision" yaml:"decision"`
	Findings     []domain.Finding      `json:"findings" yaml:"findings"`
	AgentActions []string              `json:"agent_actions" yaml:"agent_actions"`
}

func (s *ReviewService) ReviewComments(commentsPath string) (ReviewCommentReport, string, error) {
	data, err := os.ReadFile(commentsPath)
	if err != nil {
		return ReviewCommentReport{}, "", err
	}
	findings := reviewFindingsFromText(string(data))
	decision := domain.Approved
	for _, finding := range findings {
		switch strings.ToLower(finding.Severity) {
		case "critical":
			decision = domain.Rejected
		case "high", "medium":
			if decision != domain.Rejected {
				decision = domain.ChangesRequested
			}
		}
	}
	report := ReviewCommentReport{
		Decision:     decision,
		Findings:     findings,
		AgentActions: reviewCommentActions(findings),
	}
	return report, renderReviewCommentReport(report), nil
}

func reviewFindingsFromText(text string) []domain.Finding {
	lines := splitReviewCommentLines(text)
	var findings []domain.Finding
	for _, line := range lines {
		lower := strings.ToLower(line)
		severity := reviewSeverity(lower)
		if severity == "" {
			continue
		}
		findings = append(findings, domain.Finding{
			Severity: severity,
			Title:    reviewTitle(lower),
			Message:  strings.TrimSpace(line),
			Path:     reviewPath(line),
		})
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return severityRank(findings[i].Severity) < severityRank(findings[j].Severity)
	})
	return findings
}

func splitReviewCommentLines(text string) []string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimLeft(raw, "-*#> "))
		if len(line) < 8 {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 && strings.TrimSpace(text) != "" {
		out = []string{strings.TrimSpace(text)}
	}
	return out
}

func reviewSeverity(line string) string {
	switch {
	case reviewContainsAny(line, "🚫", "block", "critical", "must not ship", "auth bypass", "secret", "sql injection", "tenant isolation", "data loss"):
		return "critical"
	case reviewContainsAny(line, "changes requested", "missing test", "n+1", "unbounded", "pagination", "race", "transaction", "contract violation", "missing context"):
		return "high"
	case reviewContainsAny(line, "performance", "observability", "logging", "timeout", "cache key", "panic recovery", "rate limiting"):
		return "medium"
	case reviewContainsAny(line, "nit", "suggestion", "consider"):
		return "low"
	default:
		return ""
	}
}

func reviewTitle(line string) string {
	switch {
	case reviewContainsAny(line, "secret", "credential", "api key"):
		return "Secret exposure risk"
	case reviewContainsAny(line, "auth", "authorization", "tenant", "idor", "permission"):
		return "Authorization or isolation risk"
	case reviewContainsAny(line, "n+1", "dataloader", "batch"):
		return "GraphQL N+1 or batching risk"
	case reviewContainsAny(line, "schema", "contract", "proto", "route"):
		return "Public contract risk"
	case reviewContainsAny(line, "test", "regression"):
		return "Required tests missing"
	case reviewContainsAny(line, "race", "transaction", "inconsistent"):
		return "Data integrity risk"
	case reviewContainsAny(line, "context", "timeout", "goroutine", "listener", "connection"):
		return "Resource or context propagation risk"
	default:
		return "Review comment requires action"
	}
}

func reviewPath(line string) string {
	for _, token := range strings.Fields(line) {
		token = strings.Trim(token, "`:,()[]")
		if strings.Contains(token, "/") || strings.Contains(token, "\\") {
			if strings.Contains(token, ".go") || strings.Contains(token, ".graphql") || strings.Contains(token, ".proto") || strings.Contains(token, ".sql") {
				return token
			}
		}
	}
	return ""
}

func reviewCommentActions(findings []domain.Finding) []string {
	if len(findings) == 0 {
		return []string{"No actionable blocking review comments detected. Run review_diff before final response."}
	}
	actions := []string{
		"Fix critical/block comments first; do not work on nits while blockers remain.",
		"Open only files referenced by the comment and the current context pack top files.",
		"For each fix, add or adjust focused tests before broad refactors.",
		"After changes, run focused tests, then review_diff.",
	}
	if hasReviewTitle(findings, "Authorization or isolation risk") {
		actions = append(actions, "Verify tenant/project isolation and negative authorization cases.")
	}
	if hasReviewTitle(findings, "GraphQL N+1 or batching risk") {
		actions = append(actions, "Use DataLoader/batched fetchers; avoid resolver-per-row queries.")
	}
	if hasReviewTitle(findings, "Public contract risk") {
		actions = append(actions, "Do not change schema/proto/routes without explicit human approval.")
	}
	return actions
}

func renderReviewCommentReport(report ReviewCommentReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review comments decision: %s\n", report.Decision)
	if len(report.Findings) == 0 {
		b.WriteString("Findings: none\n")
	} else {
		b.WriteString("Findings:\n")
		for _, finding := range report.Findings {
			fmt.Fprintf(&b, "- [%s] %s", finding.Severity, finding.Title)
			if finding.Path != "" {
				fmt.Fprintf(&b, " (%s)", finding.Path)
			}
			fmt.Fprintf(&b, ": %s\n", finding.Message)
		}
	}
	b.WriteString("Agent actions:\n")
	for _, action := range report.AgentActions {
		fmt.Fprintf(&b, "- %s\n", action)
	}
	return b.String()
}

func hasReviewTitle(findings []domain.Finding, title string) bool {
	for _, finding := range findings {
		if finding.Title == title {
			return true
		}
	}
	return false
}

func reviewContainsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}
