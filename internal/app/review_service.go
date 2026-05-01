package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/adapters/rules"
	"github.com/NanoCycles/agent-brain/internal/domain"
)

type ReviewService struct {
	ruleLoader rules.Loader
}

func NewReviewService() *ReviewService {
	return &ReviewService{ruleLoader: rules.Loader{}}
}

type ReviewReport struct {
	Decision domain.ReviewDecision
	Findings []domain.Finding
}

func (s *ReviewService) ReviewPlan(planPath, rulesDir string) (ReviewReport, error) {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return ReviewReport{}, err
	}
	sets, err := s.ruleLoader.Load(rulesDir)
	if err != nil {
		return ReviewReport{}, err
	}
	topics := DetectTopics(string(data))
	var findings []domain.Finding
	for _, r := range rules.Match(rules.Flatten(sets), topics) {
		if strings.EqualFold(r.Severity, "critical") || strings.EqualFold(r.Severity, "high") {
			findings = append(findings, domain.Finding{Severity: r.Severity, Title: r.Title, Message: "Plan must explicitly address this rule.", RuleID: r.ID})
		}
	}
	decision := domain.Approved
	if len(findings) > 0 {
		decision = domain.ChangesRequested
	}
	return ReviewReport{Decision: decision, Findings: findings}, nil
}

func (s *ReviewService) ReviewDiff(ctx context.Context, repoRoot, rulesDir string) (ReviewReport, string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "diff", "--name-status")
	out, err := cmd.Output()
	if err != nil {
		return ReviewReport{}, "", err
	}
	var findings []domain.Finding
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		path := parts[len(parts)-1]
		files = append(files, path)
		if filesystem.IsForbiddenPath(path) {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "Forbidden file modified", Message: "Diff includes a path that agent-brain treats as secret or unsafe.", Path: path})
		}
	}
	hasTests := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			hasTests = true
		}
	}
	if len(files) > 0 && !hasTests {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "No tests detected", Message: "Current diff does not include *_test.go files."})
	}
	decision := domain.Approved
	for _, f := range findings {
		if f.Severity == "critical" {
			decision = domain.Rejected
			break
		}
		decision = domain.ChangesRequested
	}
	return ReviewReport{Decision: decision, Findings: findings}, renderDiffSummary(files, findings), nil
}

func renderDiffSummary(files []string, findings []domain.Finding) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Modified files: %d\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "- %s (%s)\n", f, layerForPath(f))
	}
	if len(findings) > 0 {
		b.WriteString("Findings:\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "- [%s] %s: %s\n", f.Severity, f.Title, f.Message)
		}
	}
	return b.String()
}

func layerForPath(path string) string {
	p := filepath.ToSlash(path)
	switch {
	case strings.Contains(p, "internal/domain"):
		return "domain"
	case strings.Contains(p, "internal/application"), strings.Contains(p, "internal/usecase"):
		return "application"
	case strings.Contains(p, "graphql"):
		return "adapter_graphql"
	case strings.Contains(p, "grpc"):
		return "adapter_grpc"
	case strings.Contains(p, "events"):
		return "adapter_events"
	case strings.Contains(p, "postgres"), strings.Contains(p, "memory"):
		return "adapter_persistence"
	case strings.HasPrefix(p, "cmd/"):
		return "entrypoint"
	default:
		return "unknown"
	}
}
