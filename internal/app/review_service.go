package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	if touchesPublicBoundaryTopic(plan) && !strings.Contains(plan, "validation") && !strings.Contains(plan, "validate") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Boundary validation missing", Message: "Plans touching HTTP/GraphQL/RPC/event boundaries must explicitly address input validation."})
	}
	if touchesPublicBoundaryTopic(plan) && !strings.Contains(plan, "auth") && !strings.Contains(plan, "permission") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Boundary authorization missing", Message: "Plans touching public boundaries must state how existing auth/authz remains enforced."})
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
	if err := ensureGitWorkTree(ctx, repoRoot); err != nil {
		return ReviewReport{}, "", err
	}
	out, err := runGit(ctx, repoRoot, "diff", "--name-status", "--no-ext-diff")
	if err != nil {
		return ReviewReport{}, "", err
	}
	addedByFile, _ := addedLinesByFile(ctx, repoRoot)
	untracked, _ := untrackedFiles(ctx, repoRoot)
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
		findings = append(findings, enterpriseDiffFindings(repoRoot, path, addedByFile[filepath.ToSlash(path)])...)
	}
	for _, path := range untracked {
		if containsString(files, path) {
			continue
		}
		files = append(files, path)
		if filesystem.IsForbiddenPath(path) {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "Forbidden file modified", Message: "Diff includes a path that agent-brain treats as secret or unsafe.", Path: path})
			continue
		}
		addedText := addedByFile[path]
		if addedText == "" {
			addedText = readSmallTextFile(filepath.Join(repoRoot, filepath.FromSlash(path)), 256*1024)
		}
		findings = append(findings, contractDiffFindings(repoRoot, path)...)
		findings = append(findings, enterpriseDiffFindings(repoRoot, path, addedText)...)
	}
	hasTests := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			hasTests = true
		}
	}
	relatedTests := relatedExistingTests(repoRoot, files, 8)
	if hasTestableChanges(files) && !hasTests && len(relatedTests) == 0 {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "No tests detected", Message: "Current diff does not include *_test.go files and no related existing tests were found near the changed code."})
	}
	decision := domain.Approved
	for _, f := range findings {
		if f.Severity == "critical" {
			decision = domain.Rejected
			break
		}
		decision = domain.ChangesRequested
	}
	return ReviewReport{Decision: decision, Findings: findings}, renderDiffSummary(files, findings, relatedTests), nil
}

func untrackedFiles(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := runGit(ctx, repoRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path := filepath.ToSlash(strings.TrimSpace(line))
		if path == "" || shouldSkipReviewPath(path) {
			continue
		}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func shouldSkipReviewPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if shouldSkipReviewWalkDir(part) {
			return true
		}
	}
	return false
}

func readSmallTextFile(path string, limit int64) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > limit {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return ""
	}
	return string(data)
}

func hasTestableChanges(files []string) bool {
	for _, f := range files {
		p := filepath.ToSlash(strings.ToLower(f))
		switch {
		case strings.HasSuffix(p, "_test.go"):
			continue
		case strings.HasSuffix(p, ".go"), strings.HasSuffix(p, ".graphql"), strings.HasSuffix(p, ".proto"), strings.HasSuffix(p, ".sql"), strings.HasSuffix(p, ".yaml"), strings.HasSuffix(p, ".yml"), strings.HasSuffix(p, ".json"):
			return true
		}
	}
	return false
}

func relatedExistingTests(repoRoot string, changed []string, limit int) []string {
	if limit <= 0 {
		limit = 8
	}
	changedSignals := changedTestSignals(changed)
	if len(changedSignals.Dirs) == 0 && len(changedSignals.Tokens) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	_ = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && shouldSkipReviewWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		lower := strings.ToLower(rel)
		if !strings.HasSuffix(lower, "_test.go") || filesystem.IsForbiddenPath(rel) {
			return nil
		}
		if testPathMatchesSignals(lower, changedSignals) {
			if _, ok := seen[rel]; !ok {
				out = append(out, rel)
				seen[rel] = struct{}{}
			}
		}
		return nil
	})
	sortRelatedTests(out, changedSignals)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

type testSignals struct {
	Dirs   []string
	Tokens []string
}

func changedTestSignals(files []string) testSignals {
	var signals testSignals
	for _, f := range files {
		p := filepath.ToSlash(strings.ToLower(f))
		if strings.HasSuffix(p, "_test.go") || filesystem.IsForbiddenPath(p) {
			continue
		}
		dir := filepath.Dir(p)
		if dir != "." {
			signals.Dirs = appendUnique(signals.Dirs, dir)
			parent := filepath.Dir(dir)
			if parent != "." && parent != dir {
				signals.Dirs = appendUnique(signals.Dirs, parent)
			}
		}
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		for _, token := range strings.FieldsFunc(base+" "+dir, func(r rune) bool {
			return r == '_' || r == '-' || r == '/' || r == '\\' || r == '.'
		}) {
			if len(token) >= 4 && !isWeakTestToken(token) {
				signals.Tokens = appendUnique(signals.Tokens, token)
			}
		}
	}
	return signals
}

func testPathMatchesSignals(testPath string, signals testSignals) bool {
	for _, dir := range signals.Dirs {
		if strings.HasPrefix(testPath, dir+"/") {
			return true
		}
	}
	matches := 0
	for _, token := range signals.Tokens {
		if strings.Contains(testPath, token) {
			matches++
		}
	}
	return matches >= 2
}

func shouldSkipReviewWalkDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "dist", "build", "target", "bin", "coverage", ".agent-brain":
		return true
	default:
		return false
	}
}

func isWeakTestToken(token string) bool {
	switch token {
	case "internal", "application", "infrastructure", "adapters", "secondary", "primary", "service", "services", "handler", "server", "generated", "models":
		return true
	default:
		return false
	}
}

func sortRelatedTests(tests []string, signals testSignals) {
	score := func(path string) int {
		score := 0
		for _, dir := range signals.Dirs {
			if strings.HasPrefix(path, dir+"/") {
				score += 10
			}
		}
		for _, token := range signals.Tokens {
			if strings.Contains(path, token) {
				score += 3
			}
		}
		return score
	}
	sort.Slice(tests, func(i, j int) bool {
		si, sj := score(tests[i]), score(tests[j])
		if si == sj {
			return tests[i] < tests[j]
		}
		return si > sj
	})
}

func addedLinesByFile(ctx context.Context, repoRoot string) (map[string]string, error) {
	out, err := runGit(ctx, repoRoot, "diff", "--unified=0", "--no-ext-diff")
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	var current string
	var added []string
	flush := func() {
		if current != "" {
			result[current] = strings.Join(added, "\n")
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = ""
			added = nil
			continue
		}
		if strings.HasPrefix(line, "+++ b/") {
			current = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if current == "" || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	flush()
	return result, nil
}

func ensureGitWorkTree(ctx context.Context, repoRoot string) error {
	out, err := runGit(ctx, repoRoot, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("repository root is not inside a git work tree: %s", repoRoot)
	}
	return nil
}

func runGit(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("repository root is required")
	}
	gitArgs := append([]string{"-C", repoRoot, "-c", "core.quotepath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("git %s timed out or was cancelled for repo %s: %w", strings.Join(args, " "), repoRoot, ctx.Err())
	}
	return nil, fmt.Errorf("git %s failed for repo %s: %s", strings.Join(args, " "), repoRoot, msg)
}

func enterpriseDiffFindings(repoRoot, path, addedText string) []domain.Finding {
	p := filepath.ToSlash(strings.ToLower(path))
	if strings.TrimSpace(addedText) == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		return nil
	}
	fullText := strings.ToLower(string(data))
	text := strings.ToLower(addedText)
	codeText := stripQuotedStrings(text)
	testPath := isTestPath(p)
	var findings []domain.Finding
	if !testPath && strings.HasSuffix(p, ".go") && containsDebugPrint(codeText) {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Debug print in production path", Message: "Review protocol blocks fmt.Println/println debug output in production code paths. Use structured logging if needed.", Path: path})
	}
	if strings.Contains(p, "graphql") {
		if mentionsGraphQLRelations(text) && !strings.Contains(fullText, "dataloader") && !strings.Contains(fullText, "batch") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "GraphQL relation batching not evident", Message: "GraphQL relation code appears to lack DataLoader/batched fetching evidence; external review may block N+1 risk.", Path: path})
		}
		if touchesGraphQLPublicExecution(p, text) && !reviewTextContainsAny(fullText, "depth", "complexity", "cost") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "GraphQL DoS controls not evident", Message: "Public GraphQL execution changes should show query depth/complexity/cost controls remain enforced.", Path: path})
		}
		if strings.Contains(text, "subscription") && strings.Contains(text, "tenant") && !strings.Contains(fullText, "auth") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "Subscription tenant/auth revalidation unclear", Message: "Subscription code references tenant behavior without visible auth revalidation.", Path: path})
		}
		if strings.Contains(text, "websocket") && !strings.Contains(fullText, "connection_init") && !strings.Contains(fullText, "auth") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "WebSocket auth handshake unclear", Message: "GraphQL WebSocket code should validate connection_init/auth before operations.", Path: path})
		}
	}
	if touchesCachePathOrText(p, text) && (strings.Contains(text, "tenant") || strings.Contains(text, "project")) && !cacheKeyLooksScoped(fullText) {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Cache key tenant scope unclear", Message: "Cache changes for tenant/project data should make tenant/project scope visible in the cache key.", Path: path})
	}
	if !testPath && touchesPublicBoundaryPathOrText(p, text) && !reviewTextContainsAny(fullText, "auth", "authorize", "permission", "middleware") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Boundary authorization not evident", Message: "Public boundary changes should visibly preserve auth/authz middleware or permission checks.", Path: path})
	}
	if !testPath && touchesPublicBoundaryPathOrText(p, text) && !reviewTextContainsAny(fullText, "valid", "bind", "decode", "sanitize") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Boundary input validation not evident", Message: "HTTP/GraphQL/RPC/event boundary changes should visibly validate or decode inputs safely.", Path: path})
	}
	if !testPath && isReviewCodeOrConfigPath(p) && touchesEventPathOrText(p, codeText) && !reviewTextContainsAny(fullText, "idempot", "dedup", "processed", "duplicate") {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "Event idempotency not evident", Message: "Event consumer/producer changes must show duplicate delivery/idempotency handling.", Path: path})
	}
	if touchesAuditPathOrText(p, text) && !strings.Contains(fullText, "transaction") && !strings.Contains(fullText, "tx.") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Audit transactional consistency unclear", Message: "Audit changes should show how audit writes stay consistent with the data write.", Path: path})
	}
	findings = append(findings, architectureBoundaryFindings(p, fullText, path)...)
	if !testPath {
		findings = append(findings, secretAndInjectionFindings(p, text, codeText, path)...)
	}
	findings = append(findings, contextAndResourceFindings(p, fullText, codeText, testPath, path)...)
	if !testPath && (strings.Contains(codeText, "context.todo()") || strings.Contains(codeText, "context.background()")) {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Context propagation risk", Message: "Production paths should propagate request context instead of creating background/TODO contexts.", Path: path})
	}
	return findings
}

func isTestPath(path string) bool {
	path = filepath.ToSlash(strings.ToLower(path))
	return strings.HasSuffix(path, "_test.go") ||
		strings.Contains(path, ".test.") ||
		strings.Contains(path, ".spec.") ||
		strings.Contains(path, "/testdata/")
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
	codeText := stripQuotedStrings(text)
	if !isTestPath(p) && looksLikeRESTSurfacePathOrText(p, codeText) && (strings.Contains(codeText, "handlefunc(") || strings.Contains(codeText, ".get(") || strings.Contains(codeText, ".post(") || strings.Contains(codeText, ".put(") || strings.Contains(codeText, ".patch(") || strings.Contains(codeText, ".delete(")) {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "REST route registration modified", Message: "Diff appears to touch REST route registration. Verify public route contract and integration tests.", Path: path})
	}
	if looksLikeEventContractPath(p) && strings.Contains(text, "type ") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Event contract area modified", Message: "Diff appears to touch event consumer/producer types. Verify idempotency, duplicate delivery, and payload safety.", Path: path})
	}
	return findings
}

func architectureBoundaryFindings(path, fullText, originalPath string) []domain.Finding {
	var findings []domain.Finding
	switch {
	case strings.Contains(path, "internal/domain"):
		if reviewTextContainsAny(fullText, "internal/adapters", "internal/infrastructure", "github.com/gin-gonic", "net/http", "database/sql", "graphql", "grpc") {
			findings = append(findings, domain.Finding{Severity: "critical", Title: "Domain layer depends on framework/infrastructure", Message: "Domain code must remain framework-free and must not import adapters, transport, persistence, or generated infrastructure.", Path: originalPath})
		}
	case strings.Contains(path, "internal/application"), strings.Contains(path, "internal/usecase"):
		if reviewTextContainsAny(fullText, "github.com/gin-gonic", "internal/adapters", "internal/infrastructure/adapters", "graphql", "grpc", "net/http") {
			findings = append(findings, domain.Finding{Severity: "high", Title: "Application layer transport dependency", Message: "Use cases/application services must remain transport-free and depend on ports, not HTTP/GraphQL/gRPC/DB adapters.", Path: originalPath})
		}
	}
	return findings
}

func secretAndInjectionFindings(path, text, codeText, originalPath string) []domain.Finding {
	var findings []domain.Finding
	if containsHardcodedSecretAssignment(text, codeText) {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "Possible hardcoded secret", Message: "Added code appears to assign a credential/secret-like value. Secrets must not be committed or logged.", Path: originalPath})
	}
	if reviewTextContainsAny(codeText, "fmt.sprintf", "+ \"select", "+ \" update", "+ \"insert", "+ \"delete", "where \" +", "order by \" +") && reviewTextContainsAny(path, "repository", "store", "dao", "postgres", "mysql", "sqlite", "query") {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "Possible SQL injection", Message: "Dynamic SQL construction in persistence code should use parameters/query builders and validate identifiers.", Path: originalPath})
	}
	if containsRiskyCommandConstruction(text) {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "Possible command injection", Message: "Command arguments must not be built from concatenated or formatted untrusted input.", Path: originalPath})
	}
	if containsSensitiveLoggingRisk(text, codeText) {
		findings = append(findings, domain.Finding{Severity: "critical", Title: "Sensitive data logging risk", Message: "Logs must not include secrets, tokens, cookies, authorization headers, or full sensitive payloads.", Path: originalPath})
	}
	return findings
}

func containsSensitiveLoggingRisk(text, codeText string) bool {
	codeLines := strings.Split(codeText, "\n")
	rawLines := strings.Split(strings.ToLower(text), "\n")
	for i, codeLine := range codeLines {
		codeLine = strings.TrimSpace(codeLine)
		if !reviewTextContainsAny(codeLine, "log.", "slog.", "zap.", "fmt.print", "fmt.fprint") {
			continue
		}
		rawLine := codeLine
		if i < len(rawLines) {
			rawLine = rawLines[i]
		}
		if containsSensitiveTerm(rawLine) {
			return true
		}
	}
	return false
}

func containsSensitiveTerm(line string) bool {
	line = strings.ToLower(line)
	terms := []string{"password", "passwd", "api_key", "apikey", "secret", "authorization", "cookie", "private_key", "access_token", "refresh_token", "auth_token"}
	for _, term := range terms {
		if strings.Contains(line, term) {
			return true
		}
	}
	for _, field := range strings.FieldsFunc(line, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		if field == "token" {
			return true
		}
	}
	return false
}

func containsHardcodedSecretAssignment(text, codeText string) bool {
	codeLines := strings.Split(codeText, "\n")
	rawLines := strings.Split(text, "\n")
	for i, codeLine := range codeLines {
		codeLine = strings.TrimSpace(codeLine)
		if !reviewTextContainsAny(codeLine, "api_key", "apikey", "secret", "password", "passwd", "token", "private_key") {
			continue
		}
		if !reviewTextContainsAny(codeLine, "=", ":=", "const ", "var ") {
			continue
		}
		if strings.Contains(codeLine, "==") || strings.Contains(codeLine, "!=") {
			continue
		}
		if strings.Contains(codeLine, "reviewtextcontainsany") || strings.Contains(codeLine, "strings.contains") {
			continue
		}
		if strings.Contains(codeLine, "os.getenv(") || strings.Contains(codeLine, ".header.set(") {
			continue
		}
		rawLine := codeLine
		if i < len(rawLines) {
			rawLine = rawLines[i]
		}
		if reviewTextContainsAny(rawLine, `"`, "`") {
			return true
		}
	}
	return false
}

func containsRiskyCommandConstruction(codeText string) bool {
	for _, line := range strings.Split(codeText, "\n") {
		line = strings.TrimSpace(line)
		if !reviewTextContainsAny(line, "exec.command(", "exec.commandcontext(") {
			continue
		}
		if strings.Contains(line, "fmt.sprintf") || reviewTextContainsAny(line, `"sh", "-c"`, `"bash", "-c"`, `"cmd", "/c"`, `"powershell", "-command"`) {
			return true
		}
	}
	return false
}

func contextAndResourceFindings(path, fullText, codeText string, testPath bool, originalPath string) []domain.Finding {
	if testPath {
		return nil
	}
	var findings []domain.Finding
	if reviewTextContainsAny(codeText, "http.get(", "http.post(", "http.head(", "http.defaultclient.do(") && !reviewTextContainsAny(codeText, "newrequestwithcontext", "request.withcontext") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "HTTP call lacks context", Message: "Production HTTP calls should use NewRequestWithContext and bounded clients/timeouts.", Path: originalPath})
	}
	if strings.Contains(codeText, "exec.command(") && !strings.Contains(codeText, "exec.commandcontext(") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Command execution lacks context", Message: "Production command execution should use exec.CommandContext.", Path: originalPath})
	}
	if reviewTextContainsAny(codeText, ".query(", ".queryrow(", ".exec(") && !reviewTextContainsAny(codeText, ".querycontext(", ".queryrowcontext(", ".execcontext(") && touchesPersistencePathOrText(path, codeText) {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Database call lacks context", Message: "Persistence code should use context-aware DB calls.", Path: originalPath})
	}
	if strings.Contains(codeText, "os.open(") && !strings.Contains(fullText, ".close()") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "File handle close not evident", Message: "Opened files must be closed on all paths.", Path: originalPath})
	}
	if reviewTextContainsAny(codeText, "time.newticker(", "time.newtimer(") && !strings.Contains(fullText, ".stop()") {
		findings = append(findings, domain.Finding{Severity: "high", Title: "Timer/ticker stop not evident", Message: "Timers and tickers should be stopped to avoid resource leaks.", Path: originalPath})
	}
	if strings.Contains(codeText, "go func(") && !reviewTextContainsAny(fullText, "ctx.done()", "recover()", "errgroup", "waitgroup") {
		findings = append(findings, domain.Finding{Severity: "medium", Title: "Goroutine lifecycle unclear", Message: "New goroutines should show cancellation, ownership, or panic/error handling.", Path: originalPath})
	}
	return findings
}

func stripQuotedStrings(text string) string {
	var b strings.Builder
	inSingle := false
	inDouble := false
	inRaw := false
	escaped := false
	for _, r := range text {
		switch {
		case inRaw:
			if r == '`' {
				inRaw = false
				b.WriteRune(' ')
			}
			continue
		case inSingle:
			if !escaped && r == '\'' {
				inSingle = false
				b.WriteRune(' ')
			}
			escaped = !escaped && r == '\\'
			continue
		case inDouble:
			if !escaped && r == '"' {
				inDouble = false
				b.WriteRune(' ')
			}
			escaped = !escaped && r == '\\'
			continue
		case r == '`':
			inRaw = true
			b.WriteRune(' ')
		case r == '\'':
			inSingle = true
			escaped = false
			b.WriteRune(' ')
		case r == '"':
			inDouble = true
			escaped = false
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsDebugPrint(text string) bool {
	if strings.Contains(text, "fmt.println(") {
		return true
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "println(") || strings.Contains(line, " println(") || strings.Contains(line, "\tprintln(") {
			return true
		}
	}
	return false
}

func looksLikeRESTSurfacePathOrText(path, text string) bool {
	return strings.Contains(path, "/http/") ||
		strings.Contains(path, "/rest/") ||
		strings.Contains(path, "/routes/") ||
		strings.Contains(path, "/router/") ||
		strings.Contains(path, "/handler") ||
		strings.Contains(text, "net/http") ||
		strings.Contains(text, "gin.") ||
		strings.Contains(text, "chi.") ||
		strings.Contains(text, "echo.") ||
		strings.Contains(text, "handlefunc(")
}

func mentionsGraphQLRelations(text string) bool {
	return strings.Contains(text, "relation") || strings.Contains(text, "relationship") || strings.Contains(text, "resolver") || strings.Contains(text, "nested")
}

func touchesCacheTopic(text string) bool {
	return strings.Contains(text, "cache") || strings.Contains(text, "redis")
}

func touchesPublicBoundaryTopic(text string) bool {
	return reviewTextContainsAny(text, "http", "rest", "route", "handler", "graphql", "resolver", "grpc", "rpc", "event", "consumer", "producer", "websocket")
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

func touchesPersistencePathOrText(path, text string) bool {
	return reviewTextContainsAny(path, "repository", "store", "dao", "postgres", "mysql", "sqlite", "persistence") ||
		reviewTextContainsAny(text, "database/sql", "sql.", "db.")
}

func touchesPublicBoundaryPathOrText(path, text string) bool {
	return reviewTextContainsAny(path, "/http/", "/rest/", "/handler", "/handlers/", "/router", "/routes/", "/graphql/", "/grpc/", "/proto/", "/events/", "/consumer", "/producer", "/websocket") ||
		reviewTextContainsAny(text, "handlefunc(", "gin.", "chi.", "echo.", "resolver", "graphql", "grpc", "websocket", "consumer", "producer")
}

func touchesGraphQLPublicExecution(path, text string) bool {
	return reviewTextContainsAny(path, "graphql", "resolver", "schema") && reviewTextContainsAny(text, "resolver", "query", "schema", "handler", "server")
}

func touchesEventPathOrText(path, text string) bool {
	return reviewTextContainsAny(path, "/event", "/events", "/consumer", "/producer", "/kafka", "/pubsub", "/broker") ||
		reviewTextContainsAny(text, "consumer", "producer", "publish", "subscribe", "kafka", "pubsub", "broker")
}

func isReviewCodeOrConfigPath(path string) bool {
	switch filepath.Ext(strings.ToLower(path)) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".java", ".kt", ".py", ".rb", ".rs", ".yaml", ".yml", ".json", ".proto", ".graphql", ".graphqls", ".sql":
		return true
	default:
		return false
	}
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

func reviewTextContainsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func renderDiffSummary(files []string, findings []domain.Finding, relatedTests []string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Modified files: %d\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "- %s (%s)\n", f, layerForPath(f))
	}
	if len(relatedTests) > 0 {
		b.WriteString("Existing related tests to run:\n")
		for _, t := range relatedTests {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}
	if len(findings) > 0 {
		b.WriteString("Findings:\n")
		for _, f := range findings {
			if f.Path != "" {
				fmt.Fprintf(&b, "- [%s] %s (%s): %s\n", f.Severity, f.Title, f.Path, f.Message)
			} else {
				fmt.Fprintf(&b, "- [%s] %s: %s\n", f.Severity, f.Title, f.Message)
			}
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
