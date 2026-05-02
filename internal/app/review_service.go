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
	task := AnalyzeTextAsTask("plan", string(data))
	analysis := AnalyzeTask(task)
	var findings []domain.Finding
	for _, r := range rules.Match(rules.Flatten(sets), topics) {
		if strings.EqualFold(r.Severity, "critical") || strings.EqualFold(r.Severity, "high") {
			findings = append(findings, domain.Finding{Severity: r.Severity, Title: r.Title, Message: "Plan must explicitly address this rule.", RuleID: r.ID})
		}
	}
	plan := strings.ToLower(string(data))
	if analysis.Type == domain.TaskTypeBug && !strings.Contains(plan, "regression") && !strings.Contains(plan, "_test.go") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Regression test missing", Message: "Bug plans must mention a regression test that reproduces the defect."})
	}
	if containsString(analysis.ContractImpact, "GraphQL") && !strings.Contains(plan, "contract") && !strings.Contains(plan, "schema") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Public contract impact not addressed", Message: "Plan should explicitly state whether GraphQL schema/contract changes are required."})
	}
	if strings.Contains(plan, "schema") && !strings.Contains(plan, "approval") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Schema approval missing", Message: "Plans that touch schema must require human approval."})
	}
	if analysis.MainCapability == "graphql" && hasTopic(analysis, "nested count") && !strings.Contains(plan, "n+1") && !strings.Contains(plan, "batch") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "GraphQL count performance risk missing", Message: "Plan should address N+1 count queries or batching for nested counts."})
	}
	if analysis.MainCapability == "events" && !strings.Contains(plan, "idempot") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Event idempotency missing", Message: "Event plans must mention idempotency and duplicate delivery handling."})
	}
	if (analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "authorization")) && !strings.Contains(plan, "tenant") && !strings.Contains(plan, "project") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Tenant/project isolation missing", Message: "Security plans must verify tenant/project isolation."})
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
		findings = append(findings, contractDiffFindings(repoRoot, path)...)
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

func contractDiffFindings(repoRoot, path string) []domain.Finding {
	p := filepath.ToSlash(strings.ToLower(path))
	var findings []domain.Finding
	switch {
	case strings.HasSuffix(p, ".graphql"), strings.HasSuffix(p, ".graphqls"), strings.Contains(p, "schema.graphql"):
		findings = append(findings, domain.Finding{Severity: "high", Title: "GraphQL contract modified", Message: "Diff touches GraphQL schema/contract. Confirm human approval and update contract/regression tests.", Path: path})
	case strings.HasSuffix(p, ".proto"):
		findings = append(findings, domain.Finding{Severity: "high", Title: "gRPC/protobuf contract modified", Message: "Diff touches protobuf contract. Confirm human approval and update generated code/tests.", Path: path})
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		return findings
	}
	text := strings.ToLower(string(data))
	if strings.Contains(text, "handlefunc(") || strings.Contains(text, ".get(") || strings.Contains(text, ".post(") || strings.Contains(text, ".put(") || strings.Contains(text, ".patch(") || strings.Contains(text, ".delete(") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "REST route registration modified", Message: "Diff appears to touch REST route registration. Verify public route contract and integration tests.", Path: path})
	}
	if strings.Contains(text, "type ") && (strings.Contains(text, "event") || strings.Contains(text, "consumer") || strings.Contains(text, "producer")) {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Event contract area modified", Message: "Diff appears to touch event consumer/producer types. Verify idempotency, duplicate delivery, and payload safety.", Path: path})
	}
	return findings
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
