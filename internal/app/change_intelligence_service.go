package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"gopkg.in/yaml.v3"
)

type ChangeIntelligenceService struct {
	review *ReviewService
}

func NewChangeIntelligenceService() *ChangeIntelligenceService {
	return &ChangeIntelligenceService{review: NewReviewService()}
}

func BuildImplementationChecklist(analysis domain.TaskAnalysis, scope domain.ChangeScope) []string {
	items := []string{
		"Confirm the context pack quality and open only top-ranked files first.",
		"State public contracts touched or explicitly state they remain unchanged.",
		"Keep business logic out of transport adapters; route through use cases/ports when behavior changes.",
		"Add or reuse focused tests and name the exact tests in the final response.",
		"Run agent-brain review-diff before finalizing.",
	}
	if analysis.Type == domain.TaskTypeBug {
		items = append(items, "Add or adjust a regression test that fails without the fix.")
	}
	if impactsContract(analysis, "GraphQL") {
		items = append(items,
			"Do not change GraphQL schema/response shape without explicit human approval.",
			"Verify resolver changes preserve authorization, tenant/project isolation, pagination, and batching.",
		)
	}
	if analysis.MainCapability == "graphql" && hasTopic(analysis, "nested count") {
		items = append(items,
			"Verify nested count is non-null, empty relations return 0, and top-level count semantics remain unchanged.",
			"Prove the implementation avoids N+1 queries by using existing batch/DataLoader/repository count mechanisms.",
		)
	}
	if analysis.MainCapability == "rest" {
		items = append(items, "Verify request validation, auth middleware, status codes, and route compatibility.")
	}
	if impactsContract(analysis, "gRPC") {
		items = append(items, "Do not change protobuf/service contracts without explicit human approval and generated-code/tests updates.")
	}
	if impactsContract(analysis, "Events") || analysis.MainCapability == "events" {
		items = append(items, "Verify event idempotency, duplicate delivery handling, payload safety, and retry behavior.")
	}
	if impactsContract(analysis, "DB") || containsString(analysis.Capabilities, "persistence") {
		items = append(items, "Verify transactions, migrations, data integrity, and rollback/error paths.")
	}
	if analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "auth") || containsString(analysis.Capabilities, "authorization") {
		items = append(items, "Add negative authorization tests and verify tenant/project isolation.")
	}
	if containsString(analysis.Capabilities, "cache") {
		items = append(items, "Verify cache keys include tenant/project scope and invalidation remains correct.")
	}
	if analysis.Type == domain.TaskTypePerformance || hasTopic(analysis, "n+1") {
		items = append(items, "Explain complexity/resource impact and avoid unbounded memory, queries, goroutines, or retries.")
	}
	if scope.Level == domain.ChangeScopeLarge || scope.Level == domain.ChangeScopeCritical {
		items = append(items,
			"Create a short implementation plan and pass agent-brain plan-gate before editing broad areas.",
			"Use change-checkpoint after each logical batch of edits.",
		)
	}
	return uniqueStrings(items)
}

func ClassifyChangeScope(analysis domain.TaskAnalysis, caps domain.RepoCapabilities, candidates []domain.FileCandidate, taskText string) domain.ChangeScope {
	text := strings.ToLower(taskText + " " + analysis.PrimaryTopicText + " " + strings.Join(analysis.ContractImpact, " "))
	score := 10
	var reasons []string
	var approvals []string
	add := func(points int, reason string) {
		score += points
		reasons = append(reasons, reason)
	}
	if analysis.Type == domain.TaskTypeBug {
		add(5, "bug fix requires regression proof")
	}
	if analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "authorization") || containsString(analysis.Capabilities, "auth") {
		add(55, "security/auth/authorization boundary detected")
		approvals = append(approvals, "Human approval required for auth/authz behavior changes.")
	}
	for _, impact := range analysis.ContractImpact {
		switch impact {
		case "GraphQL", "REST", "gRPC", "Events", "DB":
			add(20, "public contract impact: "+impact)
			if impact == "GraphQL" || impact == "gRPC" || impact == "Events" || impact == "DB" {
				approvals = append(approvals, "Human approval required before changing "+impact+" contracts.")
			}
		}
	}
	if analysis.MainCapability == "graphql" && (hasTopic(analysis, "nested count") || hasTopic(analysis, "n+1")) {
		add(20, "GraphQL relation/count path may trigger N+1 or contract review")
	}
	if analysis.MainCapability == "events" {
		add(25, "event delivery semantics require idempotency checks")
	}
	if containsAny(text, "migration", "schema change", "breaking", "delete", "remove", "rename", "drop column") {
		add(45, "breaking or migration-like language detected")
	}
	if len(candidates) >= 6 {
		add(15, "many likely files may be affected")
	}
	if !capabilityExists(analysis.MainCapability, caps) && analysis.MainCapability != "" && analysis.MainCapability != "unknown" {
		add(20, "task domain is not strongly represented by repository capabilities")
	}
	level := domain.ChangeScopeSmall
	mode := "minimal"
	switch {
	case score >= 85:
		level = domain.ChangeScopeCritical
		mode = "gated"
	case score >= 60:
		level = domain.ChangeScopeLarge
		mode = "checkpointed"
	case score >= 35:
		level = domain.ChangeScopeMedium
		mode = "focused"
	}
	gates := []string{"context pack read", "focused tests", "review-diff"}
	if level == domain.ChangeScopeLarge || level == domain.ChangeScopeCritical {
		gates = append(gates, "plan-gate", "change-checkpoint")
	}
	if level == domain.ChangeScopeCritical {
		gates = append(gates, "human approval before contract/security changes")
	}
	return domain.ChangeScope{Level: level, Score: score, Reasons: uniqueStrings(reasons), RequiredGates: uniqueStrings(gates), AgentMode: mode, HumanApprovals: uniqueStrings(approvals)}
}

func (s *ChangeIntelligenceService) PlanGate(ctx context.Context, repoRoot, rulesDir, planPath, taskPath string) (domain.PlanGateReport, string, error) {
	if planPath == "" {
		return domain.PlanGateReport{}, "", fmt.Errorf("plan path is required")
	}
	planData, err := os.ReadFile(planPath)
	if err != nil {
		return domain.PlanGateReport{}, "", err
	}
	task := AnalyzeTextAsTask("plan", string(planData))
	if taskPath != "" {
		if t, err := ReadTask(taskPath); err == nil {
			task = t
			task.Content += "\n\nPlan:\n" + string(planData)
		}
	}
	analysis := AnalyzeTask(task)
	var files []domain.IndexedFile
	caps := DetectRepoCapabilities(repoRoot, files)
	scope := ClassifyChangeScope(analysis, caps, nil, task.Content)
	base, err := s.review.ReviewPlan(planPath, rulesDir)
	if err != nil {
		return domain.PlanGateReport{}, "", err
	}
	findings := append([]domain.Finding{}, base.Findings...)
	findings = append(findings, planGateFindings(string(planData), analysis, scope)...)
	decision := decisionFromFindings(findings)
	checklist := BuildImplementationChecklist(analysis, scope)
	report := domain.PlanGateReport{
		Decision:     decision,
		Scope:        scope,
		Checklist:    checklist,
		Findings:     findings,
		AgentActions: planGateActions(decision, scope),
	}
	summary := renderPlanGateReport(report)
	report.ReviewSummary = summary
	_ = ctx
	return report, summary, nil
}

func (s *ChangeIntelligenceService) ReviewSimulate(ctx context.Context, repoRoot, rulesDir, taskPath string) (domain.PlanGateReport, string, error) {
	task := AnalyzeTextAsTask("diff", "current git diff")
	if taskPath != "" {
		if t, err := ReadTask(taskPath); err == nil {
			task = t
		}
	}
	analysis := AnalyzeTask(task)
	report, diffSummary, err := s.review.ReviewDiff(ctx, repoRoot, rulesDir)
	if err != nil {
		return domain.PlanGateReport{}, "", err
	}
	scope := ClassifyChangeScope(analysis, domain.RepoCapabilities{}, nil, task.Content)
	findings := append([]domain.Finding{}, report.Findings...)
	findings = append(findings, simulateExternalReviewFindings(diffSummary, analysis)...)
	out := domain.PlanGateReport{
		Decision:     decisionFromFindings(findings),
		Scope:        scope,
		Checklist:    BuildImplementationChecklist(analysis, scope),
		Findings:     findings,
		AgentActions: planGateActions(decisionFromFindings(findings), scope),
	}
	summary := renderReviewSimulatorReport(out, diffSummary)
	out.ReviewSummary = summary
	return out, summary, nil
}

func planGateFindings(planText string, analysis domain.TaskAnalysis, scope domain.ChangeScope) []domain.Finding {
	plan := strings.ToLower(planText)
	var findings []domain.Finding
	mustMention := map[string]string{
		"tests":       "Plan must name focused tests or exact test strategy.",
		"review-diff": "Plan must include agent-brain review-diff before final response.",
	}
	for needle, message := range mustMention {
		if !strings.Contains(plan, needle) && !strings.Contains(plan, strings.ReplaceAll(needle, "-", " ")) {
			findings = append(findings, domain.Finding{Severity: "high", Title: "Required gate missing: " + needle, Message: message})
		}
	}
	if scope.Level == domain.ChangeScopeLarge || scope.Level == domain.ChangeScopeCritical {
		if !strings.Contains(plan, "checkpoint") {
			findings = append(findings, domain.Finding{Severity: "medium", Title: "Checkpoint missing", Message: "Large/critical changes should include change-checkpoint after each logical edit batch."})
		}
	}
	if impactsContract(analysis, "GraphQL") && strings.Contains(plan, "schema") && !strings.Contains(plan, "approval") {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "GraphQL schema approval missing", Message: "GraphQL schema changes require explicit human approval before implementation."})
	}
	if analysis.MainCapability == "graphql" && mentionsGraphQLRelations(plan) && !reviewTextContainsAny(plan, "dataloader", "batch", "preload") {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "GraphQL N+1 prevention missing", Message: "GraphQL relation plans must prove batched loading or existing preloaded data path."})
	}
	if (analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "authorization") || strings.Contains(plan, "tenant")) && !reviewTextContainsAny(plan, "negative test", "unauthorized", "forbidden", "isolation") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Security negative tests missing", Message: "Security/auth/tenant changes must include negative authorization/isolation tests."})
	}
	return findings
}

func simulateExternalReviewFindings(diffSummary string, analysis domain.TaskAnalysis) []domain.Finding {
	lower := strings.ToLower(diffSummary)
	var findings []domain.Finding
	if analysis.Type == domain.TaskTypeBug && strings.Contains(lower, "no tests detected") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "External review likely requests regression test", Message: "Bug diff does not show new tests and no related tests were detected."})
	}
	if impactsContract(analysis, "GraphQL") && strings.Contains(lower, "graphql") && !reviewTextContainsAny(lower, "dataloader", "batch", "preload") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "External review may flag GraphQL N+1", Message: "Diff touches GraphQL area; final response should prove batching/preload behavior and tests."})
	}
	if (analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "authorization")) && !reviewTextContainsAny(lower, "tenant", "permission", "auth") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "External review may flag auth coverage", Message: "Security-sensitive task lacks visible auth/tenant proof in diff summary."})
	}
	return findings
}

func decisionFromFindings(findings []domain.Finding) domain.ReviewDecision {
	decision := domain.Approved
	for _, f := range findings {
		switch strings.ToLower(f.Severity) {
		case "critical":
			return domain.Rejected
		case "high", "medium":
			decision = domain.ChangesRequested
		}
	}
	return decision
}

func planGateActions(decision domain.ReviewDecision, scope domain.ChangeScope) []string {
	if decision == domain.Approved {
		return []string{"Proceed with smallest safe change.", "Run focused tests.", "Use change-checkpoint before finalizing if scope is large or critical."}
	}
	actions := []string{"Fix plan findings before editing broad areas.", "Address critical/high findings first.", "Name the exact tests and contract/security proof in the plan."}
	if scope.Level == domain.ChangeScopeLarge || scope.Level == domain.ChangeScopeCritical {
		actions = append(actions, "Split implementation into checkpoints and run review-simulate/review-diff after each batch.")
	}
	return actions
}

func renderPlanGateReport(report domain.PlanGateReport) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Plan gate decision: %s\n", report.Decision)
	fmt.Fprintf(&b, "Change scope: %s (score %d, mode %s)\n", report.Scope.Level, report.Scope.Score, report.Scope.AgentMode)
	writeStringSection(&b, "Scope reasons", report.Scope.Reasons)
	writeStringSection(&b, "Required gates", report.Scope.RequiredGates)
	writeStringSection(&b, "Human approvals", report.Scope.HumanApprovals)
	writeFindings(&b, report.Findings)
	writeStringSection(&b, "Implementation checklist", report.Checklist)
	writeStringSection(&b, "Agent actions", report.AgentActions)
	return b.String()
}

func renderReviewSimulatorReport(report domain.PlanGateReport, diffSummary string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "External review simulator decision: %s\n", report.Decision)
	fmt.Fprintf(&b, "Change scope: %s (score %d, mode %s)\n", report.Scope.Level, report.Scope.Score, report.Scope.AgentMode)
	b.WriteString("\nDiff intelligence:\n")
	b.WriteString(strings.TrimSpace(diffSummary))
	b.WriteString("\n")
	writeFindings(&b, report.Findings)
	writeStringSection(&b, "Implementation checklist", report.Checklist)
	writeStringSection(&b, "Agent actions", report.AgentActions)
	return b.String()
}

func writeFindings(b *bytes.Buffer, findings []domain.Finding) {
	if len(findings) == 0 {
		b.WriteString("Findings: none\n")
		return
	}
	b.WriteString("Findings:\n")
	for _, f := range findings {
		if f.Path != "" {
			fmt.Fprintf(b, "- [%s] %s (%s): %s\n", f.Severity, f.Title, f.Path, f.Message)
		} else {
			fmt.Fprintf(b, "- [%s] %s: %s\n", f.Severity, f.Title, f.Message)
		}
	}
}

func writeStringSection(b *bytes.Buffer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
}

func impactsContract(analysis domain.TaskAnalysis, impact string) bool {
	for _, item := range analysis.ContractImpact {
		if item == impact {
			return true
		}
	}
	return false
}

func ReviewCommentsLearningProposal(commentsPath, outputDir string) (string, domain.DomainMemory, error) {
	data, err := os.ReadFile(commentsPath)
	if err != nil {
		return "", domain.DomainMemory{}, err
	}
	report, _, err := NewReviewService().ReviewCommentsFromText(string(data))
	if err != nil {
		return "", domain.DomainMemory{}, err
	}
	memory := domain.DomainMemory{GeneratedAt: time.Now().UTC()}
	for _, finding := range report.Findings {
		area := reviewFindingArea(finding)
		if area == "" {
			continue
		}
		memory.BusinessRules = append(memory.BusinessRules, domain.BusinessRule{
			Name:       "External review: " + finding.Title,
			Area:       area,
			Statement:  finding.Message,
			Evidence:   []string{changeValueOr(finding.Path, commentsPath)},
			Confidence: 0.65,
		})
		memory.Invariants = append(memory.Invariants, domain.BusinessInvariant{
			Name:       "Prevent repeat review finding: " + finding.Title,
			Area:       area,
			Statement:  "Future agent plans should explicitly address: " + finding.Message,
			Evidence:   []string{changeValueOr(finding.Path, commentsPath)},
			Confidence: 0.62,
		})
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", domain.DomainMemory{}, err
	}
	id := strings.TrimSuffix(filepath.Base(commentsPath), filepath.Ext(commentsPath))
	path := filepath.Join(outputDir, id+".review-learning.domain.yml")
	if err := writeDomainMemoryYAML(path, memory); err != nil {
		return "", domain.DomainMemory{}, err
	}
	return path, memory, nil
}

func reviewFindingArea(f domain.Finding) string {
	text := strings.ToLower(f.Title + " " + f.Message + " " + f.Path)
	switch {
	case strings.Contains(text, "graphql"):
		return "graphql"
	case strings.Contains(text, "auth") || strings.Contains(text, "tenant") || strings.Contains(text, "permission"):
		return "auth"
	case strings.Contains(text, "event") || strings.Contains(text, "idempot"):
		return "events"
	case strings.Contains(text, "cache"):
		return "persistence"
	case strings.Contains(text, "transaction") || strings.Contains(text, "sql") || strings.Contains(text, "db"):
		return "persistence"
	default:
		return "application"
	}
}

func writeDomainMemoryYAML(path string, memory domain.DomainMemory) error {
	data, err := yaml.Marshal(memory)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func changeValueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
