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
	if analysis.MainCapability == "graphql" {
		if mentionsGraphQLRelations(plan) && !strings.Contains(plan, "dataloader") && !strings.Contains(plan, "batch") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "GraphQL relation batching missing", Message: "Plans touching GraphQL relations must explicitly use DataLoader/batched fetchers and avoid resolver-per-row queries."})
		}
		if strings.Contains(plan, "subscription") && (!strings.Contains(plan, "auth") || !strings.Contains(plan, "tenant")) {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "Subscription auth/tenant validation missing", Message: "GraphQL subscription plans must re-validate auth and tenant/project context."})
		}
		if strings.Contains(plan, "websocket") && !strings.Contains(plan, "connection_init") && !strings.Contains(plan, "handshake") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "WebSocket auth handshake missing", Message: "GraphQL WebSocket plans must validate connection_init/handshake before accepting operations."})
		}
		if (strings.Contains(plan, "query") || strings.Contains(plan, "resolver")) && !strings.Contains(plan, "complexity") && !strings.Contains(plan, "depth") {
			findings = append(findings, domain.Finding{Severity: "high", Title: "GraphQL DoS controls not addressed", Message: "Plans touching public GraphQL queries/resolvers should state whether depth/complexity limits remain enforced."})
		}
	}
	if analysis.MainCapability == "events" && !strings.Contains(plan, "idempot") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Event idempotency missing", Message: "Event plans must mention idempotency and duplicate delivery handling."})
	}
	if (analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "authorization")) && !strings.Contains(plan, "tenant") && !strings.Contains(plan, "project") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Tenant/project isolation missing", Message: "Security plans must verify tenant/project isolation."})
	}
	if touchesCacheTopic(plan) && !strings.Contains(plan, "tenant") && !strings.Contains(plan, "project") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Cache tenant scope missing", Message: "Cache changes for tenant/project data must include tenant/project scope in cache keys."})
	}
	if touchesAuditTopic(plan) && !strings.Contains(plan, "transaction") && !strings.Contains(plan, "atomic") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Audit transactional consistency missing", Message: "Audit trail changes must address transactional consistency with the data write."})
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
		findings = append(findings, enterpriseDiffFindings(repoRoot, path)...)
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

func enterpriseDiffFindings(repoRoot, path string) []domain.Finding {
	p := filepath.ToSlash(strings.ToLower(path))
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		return nil
	}
	text := strings.ToLower(string(data))
	var findings []domain.Finding
	if strings.HasSuffix(p, ".go") && (strings.Contains(text, "fmt.println(") || strings.Contains(text, "println(")) {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Debug print in production path", Message: "Review protocol blocks fmt.Println/println debug output in production code paths. Use structured logging if needed.", Path: path})
	}
	if strings.Contains(p, "graphql") {
		if mentionsGraphQLRelations(text) && !strings.Contains(text, "dataloader") && !strings.Contains(text, "batch") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "GraphQL relation batching not evident", Message: "GraphQL relation code appears to lack DataLoader/batched fetching evidence; external review may block N+1 risk.", Path: path})
		}
		if strings.Contains(text, "subscription") && strings.Contains(text, "tenant") && !strings.Contains(text, "auth") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "Subscription tenant/auth revalidation unclear", Message: "Subscription code references tenant behavior without visible auth revalidation.", Path: path})
		}
		if strings.Contains(text, "websocket") && !strings.Contains(text, "connection_init") && !strings.Contains(text, "auth") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "WebSocket auth handshake unclear", Message: "GraphQL WebSocket code should validate connection_init/auth before operations.", Path: path})
		}
	}
	if touchesCachePathOrText(p, text) && (strings.Contains(text, "tenant") || strings.Contains(text, "project")) && !cacheKeyLooksScoped(text) {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Cache key tenant scope unclear", Message: "Cache changes for tenant/project data should make tenant/project scope visible in the cache key.", Path: path})
	}
	if touchesAuditPathOrText(p, text) && !strings.Contains(text, "transaction") && !strings.Contains(text, "tx.") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Audit transactional consistency unclear", Message: "Audit changes should show how audit writes stay consistent with the data write.", Path: path})
	}
	if strings.Contains(text, "context.todo()") || strings.Contains(text, "context.background()") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Context propagation risk", Message: "Production paths should propagate request context instead of creating background/TODO contexts.", Path: path})
	}
	return findings
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
	if looksLikeEventContractPath(p) && strings.Contains(text, "type ") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Event contract area modified", Message: "Diff appears to touch event consumer/producer types. Verify idempotency, duplicate delivery, and payload safety.", Path: path})
	}
	return findings
}

func mentionsGraphQLRelations(text string) bool {
	return strings.Contains(text, "relation") || strings.Contains(text, "relationship") || strings.Contains(text, "resolver") || strings.Contains(text, "nested")
}

func touchesCacheTopic(text string) bool {
	return strings.Contains(text, "cache") || strings.Contains(text, "redis")
}

func touchesAuditTopic(text string) bool {
	return strings.Contains(text, "audit")
}

func touchesCachePathOrText(path, text string) bool {
	return strings.Contains(path, "cache") || strings.Contains(path, "redis") || strings.Contains(text, "cache")
}

func touchesAuditPathOrText(path, text string) bool {
	return strings.Contains(path, "audit") || strings.Contains(text, "audit")
}

func cacheKeyLooksScoped(text string) bool {
	return (strings.Contains(text, "tenant") || strings.Contains(text, "project")) &&
		(strings.Contains(text, "key") || strings.Contains(text, "cachekey") || strings.Contains(text, "cache key"))
}

func looksLikeEventContractPath(path string) bool {
	return strings.Contains(path, "/event") ||
		strings.Contains(path, "/events") ||
		strings.Contains(path, "/consumer") ||
		strings.Contains(path, "/producer") ||
		strings.Contains(path, "/kafka") ||
		strings.Contains(path, "/pubsub") ||
		strings.Contains(path, "/broker")
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
