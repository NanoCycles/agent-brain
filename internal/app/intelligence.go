package app

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/domain"
)

var noiseWords = map[string]struct{}{
	"title": {}, "context": {}, "problem": {}, "expected": {}, "behavior": {}, "public": {}, "contract": {}, "changes": {},
	"while": {}, "are": {}, "is": {}, "the": {}, "and": {}, "or": {}, "of": {}, "to": {}, "in": {}, "with": {}, "from": {},
	"this": {}, "that": {}, "api": {}, "exposes": {}, "returns": {}, "return": {}, "should": {}, "must": {}, "when": {},
}

var technicalTerms = map[string]int{
	"graphql": 90, "nested": 45, "count": 70, "null": 60, "relationship": 55, "relation": 45, "resolver": 65,
	"schema": 55, "pagination": 55, "connection": 45, "totalcount": 60, "list": 30, "item": 35, "rest": 65,
	"grpc": 65, "event": 65, "idempotency": 70, "consumer": 55, "producer": 55, "repository": 45,
	"adapter": 40, "usecase": 45, "port": 35, "auth": 65, "authorization": 70, "tenant": 60, "project": 45,
	"isolation": 65, "memory": 55, "concurrency": 55, "race": 55, "goroutine": 55, "cache": 45,
	"database": 45, "postgres": 50, "mysql": 50, "sqlite": 50, "neo4j": 50, "bug": 45, "regression": 50,
	"test": 40, "n+1": 80, "performance": 60, "security": 65,
}

var phraseWeights = map[string]int{
	"nested count": 110, "graphql relationship": 100, "graphql resolver": 105, "public contract": 90,
	"schema change": 95, "top-level count": 95, "event idempotency": 105, "tenant isolation": 105,
	"regression test": 85, "race condition": 90, "memory leak": 90, "goroutine leak": 90,
	"n plus one": 100, "n+1": 100,
}

func AnalyzeTask(task domain.Task) domain.TaskAnalysis {
	text := strings.ToLower(task.ID + " " + task.Title + " " + task.Content)
	topics := ExtractTechnicalTopics(text)
	caps := detectCapabilities(text, topics)
	contracts := detectContractImpact(caps, text)
	layers := affectedLayers(caps)
	return domain.TaskAnalysis{
		Type:             detectTaskType(task.ID, text),
		MainCapability:   firstOrUnknown(caps),
		Capabilities:     caps,
		ContractImpact:   contracts,
		AffectedLayers:   layers,
		TechnicalTopics:  topics,
		PrimaryTopicText: strings.Join(topicNames(topics), " "),
	}
}

func AnalyzeTextAsTask(id, text string) domain.Task {
	return domain.Task{ID: id, Title: id, Content: text, Topics: DetectTopics(text)}
}

func ExtractTechnicalTopics(text string) []domain.TechnicalTopic {
	norm := normalizeText(text)
	scores := map[string]int{}
	for phrase, weight := range phraseWeights {
		if strings.Contains(norm, phrase) {
			scores[phrase] += weight
		}
	}
	words := regexp.MustCompile(`[a-z0-9+][a-z0-9_+/-]*`).FindAllString(norm, -1)
	for _, w := range words {
		w = normalizeTopic(w)
		if w == "" {
			continue
		}
		if _, noisy := noiseWords[w]; noisy {
			continue
		}
		if weight, ok := technicalTerms[w]; ok {
			scores[w] += weight
			continue
		}
		if strings.Contains(w, "count") || strings.Contains(w, "resolver") || strings.Contains(w, "schema") {
			scores[w] += 35
		}
	}
	var out []domain.TechnicalTopic
	for name, score := range scores {
		out = append(out, domain.TechnicalTopic{Name: name, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Name < out[j].Name
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func DetectTopics(text string) []string {
	return topicNames(ExtractTechnicalTopics(text))
}

func DetectRepoCapabilities(repoRoot string, files []domain.IndexedFile) domain.RepoCapabilities {
	caps := domain.RepoCapabilities{MainLanguage: "unknown", HasGoMod: fileExists(filepath.Join(repoRoot, "go.mod"))}
	if caps.HasGoMod {
		caps.MainLanguage = "go"
	}
	layerSeen := map[string]struct{}{}
	addEvidence := func(cap, path, reason string, confidence float64) {
		caps.Evidence = append(caps.Evidence, domain.CapabilityEvidence{Capability: cap, Path: path, Reason: reason, Confidence: confidence})
		switch cap {
		case "graphql":
			caps.HasGraphQL = true
		case "rest":
			caps.HasREST = true
		case "grpc":
			caps.HasGRPC = true
		case "events":
			caps.HasEvents = true
		case "persistence":
			caps.HasPersistence = true
		case "testing":
			caps.HasTests = true
		}
	}
	for _, f := range files {
		if f.Layer != "" {
			layerSeen[f.Layer] = struct{}{}
		}
		path := filepath.ToSlash(strings.ToLower(f.Path))
		hay := path + " " + strings.ToLower(f.Package) + " " + strings.Join(f.Imports, " ") + " " + symbolText(f)
		if strings.HasSuffix(path, "_test.go") || len(f.Tests) > 0 {
			addEvidence("testing", f.Path, "test file or test function detected", 0.95)
		}
		if strings.Contains(hay, "gqlgen") || strings.Contains(path, "graphql") || strings.Contains(path, "schema.graphql") || strings.HasSuffix(path, ".graphql") || strings.Contains(path, "graph/model") || strings.Contains(path, "graph/generated") || strings.Contains(hay, "resolver") {
			addEvidence("graphql", f.Path, "GraphQL path, import, schema, generated code, or resolver symbol", 0.85)
		}
		if hasContractKind(f, "GraphQLField") {
			addEvidence("graphql", f.Path, "GraphQL contract extracted from schema or resolver", 0.95)
		}
		if strings.Contains(hay, "net/http") || strings.Contains(path, "http") || strings.Contains(path, "rest") || strings.Contains(hay, "router") || strings.Contains(hay, "gin-gonic/gin") || strings.Contains(hay, "go-chi/chi") || strings.Contains(hay, "labstack/echo") || (strings.Contains(hay, "handler") && (strings.Contains(path, "http") || strings.Contains(path, "rest"))) {
			addEvidence("rest", f.Path, "HTTP/REST path, import, handler, or router", 0.75)
		}
		if hasContractKind(f, "RESTEndpoint") {
			addEvidence("rest", f.Path, "REST endpoint extracted from route registration", 0.9)
		}
		if strings.Contains(path, "grpc") || strings.Contains(path, "proto") || strings.HasSuffix(path, ".proto") || strings.HasSuffix(path, "pb.go") || strings.Contains(hay, "google.golang.org/grpc") {
			addEvidence("grpc", f.Path, "gRPC/protobuf path or import", 0.8)
		}
		if hasContractKind(f, "GRPCMethod") {
			addEvidence("grpc", f.Path, "gRPC method extracted from protobuf contract", 0.95)
		}
		if strings.Contains(path, "event") || strings.Contains(path, "consumer") || strings.Contains(path, "producer") || strings.Contains(hay, "kafka") || strings.Contains(hay, "broker") || strings.Contains(hay, "pubsub") {
			addEvidence("events", f.Path, "eventing path or symbol", 0.7)
		}
		if hasContractKind(f, "EventType") {
			addEvidence("events", f.Path, "event type contract extracted", 0.85)
		}
		if strings.Contains(hay, "postgres") || strings.Contains(hay, "mysql") || strings.Contains(hay, "sqlite") || strings.Contains(hay, "repository") || strings.Contains(hay, "store") || strings.Contains(hay, "dao") {
			addEvidence("persistence", f.Path, "persistence path, import, or repository/store symbol", 0.7)
		}
		if hasContractPrefix(f, "DB.") {
			addEvidence("persistence", f.Path, "persistence contract extracted from repository/store type", 0.8)
		}
	}
	for layer := range layerSeen {
		caps.DetectedLayers = append(caps.DetectedLayers, layer)
	}
	sort.Strings(caps.DetectedLayers)
	return caps
}

func RankFileCandidates(ctx context.Context, repoRoot string, files []domain.IndexedFile, analysis domain.TaskAnalysis, caps domain.RepoCapabilities, matchedRules []domain.Rule) []domain.FileCandidate {
	var out []domain.FileCandidate
	ruleCaps := ruleCapabilities(matchedRules)
	for _, f := range files {
		if filesystem.IsForbiddenPath(f.Path) {
			continue
		}
		c := domain.FileCandidate{Path: f.Path, Source: "sqlite", Category: "low_confidence"}
		path := filepath.ToSlash(strings.ToLower(f.Path))
		hay := path + " " + strings.ToLower(f.Package) + " " + strings.Join(f.Imports, " ") + " " + symbolText(f)
		if capabilityExists(analysis.MainCapability, caps) && containsCapability(hay, analysis.MainCapability) {
			c.Score += 80
			c.MatchedCapabilities = appendUnique(c.MatchedCapabilities, analysis.MainCapability)
			c.Evidence = append(c.Evidence, "path or symbol matches main capability")
		}
		for _, t := range analysis.TechnicalTopics {
			if strings.Contains(hay, strings.ToLower(t.Name)) {
				c.Score += 20
				c.MatchedTopics = appendUnique(c.MatchedTopics, t.Name)
			}
		}
		if len(c.MatchedTopics) > 0 {
			c.Score += 40
			c.Evidence = append(c.Evidence, "matched technical topics")
		}
		for _, layer := range analysis.AffectedLayers {
			if f.Layer == layer {
				c.Score += 45
				c.Evidence = append(c.Evidence, "file layer matches affected layer")
			}
		}
		for _, impact := range analysis.ContractImpact {
			if contractMatchesPath(impact, path) {
				c.Score += 40
				c.Evidence = append(c.Evidence, "path matches public contract impact "+impact)
			}
			if fileHasContractImpact(f, impact) {
				c.Score += 70
				c.Evidence = append(c.Evidence, "extracted contract matches public contract impact "+impact)
				c.MatchedCapabilities = appendUnique(c.MatchedCapabilities, strings.ToLower(strings.TrimSuffix(impact, "s")))
			}
		}
		if limitedContentContains(repoRoot, f.Path, analysis.TechnicalTopics) {
			c.Score += 30
			c.Evidence = append(c.Evidence, "limited file content contains technical phrase")
		}
		if analysis.MainCapability == "graphql" && hasTopic(analysis, "nested count") {
			applyGraphQLNestedCountSpecificity(&c, path, hay)
		}
		for _, rc := range ruleCaps {
			if containsCapability(hay, rc) {
				c.Score += 20
			}
		}
		if isRelatedTest(f, out) {
			c.Score += 25
			c.Category = "related_test"
			c.Evidence = append(c.Evidence, "test appears related to a primary candidate")
		}
		if strings.HasSuffix(path, "_test.go") && testMatchesTask(f, analysis) {
			c.Score += 45
			c.Category = "related_test"
			c.Evidence = append(c.Evidence, "test name/symbol/content matches task topics or capability")
		}
		if isAgentBrainTooling(path) && analysis.MainCapability != "agent-brain" && !containsCapability(hay, analysis.MainCapability) {
			c.Score -= 50
			c.Warning = "tooling file; likely not target application code"
		}
		if isGenericInfra(path) && !strings.Contains(analysis.PrimaryTopicText, "agent-brain") {
			c.Score -= 30
		}
		if analysis.MainCapability != "" && analysis.MainCapability != "unknown" && !capabilityExists(analysis.MainCapability, caps) {
			c.Score -= 100
			c.Warning = "main task capability was not detected in this repository"
		}
		if len(c.MatchedTopics) == 0 && len(c.MatchedCapabilities) == 0 {
			c.Score -= 40
		}
		if strings.HasSuffix(path, "_test.go") && c.Category == "related_test" {
			// Keep tests distinct from implementation candidates even when they score highly.
		} else if c.Score >= 120 {
			c.Category = "primary_candidate"
		} else if c.Score >= 70 && c.Category != "related_test" {
			c.Category = "supporting_infrastructure"
		} else if isAgentBrainTooling(path) {
			c.Category = "repo_tooling"
		}
		if c.Score > 0 {
			c.Confidence = confidence(c.Score)
			c.Reason = candidateReason(c)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category && abs(out[i].Score-out[j].Score) <= 50 {
			return candidateCategoryRank(out[i].Category) < candidateCategoryRank(out[j].Category)
		}
		if out[i].Score == out[j].Score {
			return out[i].Path < out[j].Path
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func candidateCategoryRank(category string) int {
	switch category {
	case "primary_candidate":
		return 0
	case "related_test":
		return 1
	case "supporting_infrastructure":
		return 2
	case "low_confidence":
		return 3
	case "repo_tooling":
		return 4
	default:
		return 5
	}
}

func applyGraphQLNestedCountSpecificity(c *domain.FileCandidate, path, hay string) {
	specificTerms := []string{"nested_args", "nestedargs", "relationship", "relationships", "relation", "batch", "resolver", "count"}
	matches := 0
	for _, term := range specificTerms {
		if strings.Contains(path, term) || strings.Contains(hay, term) {
			matches++
		}
	}
	if matches >= 2 {
		c.Score += 70
		c.Evidence = append(c.Evidence, "strong GraphQL nested-count specific path/symbol match")
		c.MatchedTopics = appendUnique(c.MatchedTopics, "nested count")
	}
	if strings.Contains(path, "relationships.go") || strings.Contains(path, "relationship.go") {
		c.Score += 45
		c.Evidence = append(c.Evidence, "relationship transformer is likely relevant to nested count")
	}
	if strings.Contains(path, "schema.go") && !hasSchemaChangeSignal(hay) {
		c.Score -= 70
		c.Evidence = append(c.Evidence, "generic schema support; lower priority unless schema changes")
	}
	if strings.Contains(path, "test_helpers") || strings.Contains(path, "helper") {
		c.Score -= 55
		c.Evidence = append(c.Evidence, "test/helper support file; inspect after implementation and direct tests")
	}
}

func hasSchemaChangeSignal(text string) bool {
	return strings.Contains(text, "schema change") || strings.Contains(text, "schema.graphql") || strings.Contains(text, "gqlgen")
}

func ComputeContextQuality(analysis domain.TaskAnalysis, caps domain.RepoCapabilities, candidates []domain.FileCandidate, rules domain.RuleGroups, graphStatsKnown bool) domain.ContextQuality {
	score := 0.25
	var reasons, warnings []string
	if capabilityExists(analysis.MainCapability, caps) {
		score += 0.25
		reasons = append(reasons, "repository capability matches task domain")
	} else if analysis.MainCapability != "" && analysis.MainCapability != "unknown" {
		warnings = append(warnings, displayCapabilityWarning(analysis.MainCapability))
		score -= 0.15
	}
	primary := 0
	hasTests := false
	for _, c := range candidates {
		if c.Category == "primary_candidate" {
			primary++
		}
		if c.Category == "related_test" || strings.HasSuffix(c.Path, "_test.go") {
			hasTests = true
		}
	}
	if primary >= 3 {
		score += 0.25
		reasons = append(reasons, "three or more primary candidates found")
	} else if primary == 0 {
		warnings = append(warnings, "no strong relevant implementation files detected")
		score -= 0.2
	}
	if hasTests || caps.HasTests {
		score += 0.1
		reasons = append(reasons, "tests are present")
	} else {
		warnings = append(warnings, "repo is indexed but no tests were detected")
	}
	if len(rules.Architecture)+len(rules.Contracts)+len(rules.Security)+len(rules.Testing) > 0 {
		score += 0.15
		reasons = append(reasons, "applicable local rules matched")
	}
	if !graphStatsKnown {
		warnings = append(warnings, "graph may be empty or unavailable; SQLite metadata was used")
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	level := "low"
	next := "Index the target service repository or add domain-specific rules before broad code changes."
	if score >= 0.75 {
		level = "high"
		next = "Open the top ranked files and implement with focused regression tests."
	} else if score >= 0.45 {
		level = "medium"
		next = "Open top candidates, then confirm missing domain details before broad exploration."
	}
	return domain.ContextQuality{Score: score, Level: level, Reasons: reasons, Warnings: warnings, RecommendedNextAction: next}
}

func MatchRulesV2(all []domain.Rule, analysis domain.TaskAnalysis) domain.RuleGroups {
	var groups domain.RuleGroups
	add := func(r domain.Rule) {
		switch {
		case strings.HasPrefix(r.ID, "architecture."):
			groups.Architecture = appendRule(groups.Architecture, r)
		case strings.HasPrefix(r.ID, "security."):
			groups.Security = appendRule(groups.Security, r)
		case strings.HasPrefix(r.ID, "testing."):
			groups.Testing = appendRule(groups.Testing, r)
		default:
			groups.Contracts = appendRule(groups.Contracts, r)
		}
	}
	topicText := strings.Join(append(topicNames(analysis.TechnicalTopics), append(analysis.Capabilities, analysis.AffectedLayers...)...), " ")
	for _, r := range all {
		for _, rt := range r.Topics {
			if strings.Contains(topicText, strings.ToLower(rt)) || strings.Contains(strings.ToLower(r.ID+" "+r.Title), analysis.MainCapability) {
				add(r)
				break
			}
		}
	}
	for _, r := range all {
		if containsString(analysis.AffectedLayers, "adapter_graphql") && (r.ID == "architecture.thin_adapters" || r.ID == "architecture.usecase_dependencies" || strings.HasPrefix(r.ID, "graphql.")) {
			add(r)
		}
		if analysis.Type == domain.TaskTypeBug && r.ID == "testing.bug_regression" {
			add(r)
		}
		if containsString(analysis.ContractImpact, "GraphQL") && r.ID == "graphql.schema_approval" {
			add(r)
		}
	}
	return groups
}

func BuildRisks(analysis domain.TaskAnalysis) domain.RiskReport {
	if analysis.MainCapability == "graphql" && hasTopic(analysis, "nested count") {
		return domain.RiskReport{
			Security: []string{
				"Count must respect authorization and tenant/project isolation.",
				"Avoid exposing existence of hidden related records.",
			},
			Concurrency: []string{
				"Usually low unless batching/cache/dataloader/shared state is modified.",
				"If batching/cache is used, ensure request-scoped or concurrency-safe state.",
			},
			MemoryPerformance: []string{
				"Avoid loading all related records only to calculate count.",
				"Avoid N+1 count queries.",
				"Pagination must still limit items.",
			},
		}
	}
	if analysis.MainCapability == "events" {
		return domain.RiskReport{
			Security:          []string{"Event payload must not expose sensitive data."},
			Concurrency:       []string{"Duplicate delivery and retries must be safe.", "Avoid race conditions in processed-event tracking."},
			MemoryPerformance: []string{"Avoid unbounded retries/backlog."},
		}
	}
	if analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "auth") || containsString(analysis.Capabilities, "authorization") {
		return domain.RiskReport{
			Security:          []string{"Auth/authz changes require human approval.", "Verify tenant/project isolation.", "Avoid logging sensitive data."},
			Concurrency:       []string{"Review shared auth caches or token state for race safety."},
			MemoryPerformance: []string{"Avoid unbounded permission expansion or large policy loads."},
		}
	}
	return domain.RiskReport{
		Security:          []string{"Review authorization and tenant/project isolation if data visibility changes."},
		Concurrency:       []string{"Low unless shared state, cache, goroutines, or batching are modified."},
		MemoryPerformance: []string{"Avoid unbounded loading and preserve pagination or streaming behavior."},
	}
}

func SuggestTests(analysis domain.TaskAnalysis) []string {
	var tests []string
	if analysis.Type == domain.TaskTypeBug {
		tests = append(tests, "Regression test reproduces the bug.", "Existing behavior remains unchanged.")
	}
	if analysis.MainCapability == "graphql" && hasTopic(analysis, "nested count") {
		tests = append(tests,
			"Nested count is not null.",
			"Empty relation returns count 0.",
			"Nested items remain paginated.",
			"Top-level count remains unchanged.",
			"Filters affect count consistently.",
			"Authorization/tenant isolation respected if applicable.",
		)
	}
	if analysis.MainCapability == "events" {
		tests = append(tests,
			"First event processes normally.",
			"Duplicate event does not reprocess.",
			"Failed processing does not mark event processed.",
			"Invalid event returns controlled error.",
			"Concurrent duplicate events do not double process if concurrency is involved.",
		)
	}
	if len(tests) == 0 {
		tests = append(tests, "Add or update unit tests for changed behavior.")
	}
	return uniqueStrings(tests)
}

func RecommendedStrategy(analysis domain.TaskAnalysis) []string {
	if analysis.MainCapability == "graphql" && hasTopic(analysis, "nested count") {
		return []string{
			"Locate GraphQL resolver or connection/list response builder.",
			"Check whether nested count is nil, omitted, or not propagated.",
			"Confirm count semantics: total matching records before pagination unless existing API says otherwise.",
			"Preserve GraphQL schema.",
			"Do not use len(items) when pagination applies unless semantics explicitly mean returned item count.",
			"Ensure count respects filters, relation predicate, tenant/project isolation.",
			"Avoid N+1 count queries; prefer batched count or existing repository count mechanism.",
			"Add regression tests for nested count numeric and empty relation count = 0.",
			"Verify top-level count remains unchanged.",
		}
	}
	if analysis.MainCapability == "events" {
		return []string{
			"Locate event consumer and use case.",
			"Check processed event store before side effects.",
			"Return success for duplicates.",
			"Do not mark processed if side effect fails.",
			"Add duplicate event tests.",
			"Ensure context cancellation and safe retries.",
		}
	}
	if analysis.Type == domain.TaskTypeSecurity || containsString(analysis.Capabilities, "auth") || containsString(analysis.Capabilities, "authorization") {
		return []string{
			"Require human approval.",
			"Identify auth boundary.",
			"Add negative tests.",
			"Verify tenant/project isolation.",
			"Avoid logging sensitive data.",
		}
	}
	if analysis.Type == domain.TaskTypePerformance {
		return []string{
			"Identify hot path.",
			"Measure or reason about complexity.",
			"Avoid unbounded memory/concurrency.",
			"Add benchmark only if valuable.",
		}
	}
	return []string{"Open top ranked files first.", "Make the smallest behavior-preserving change.", "Add focused tests for the requested behavior."}
}

func BuildContextPack(ctx context.Context, repoRoot string, metaFiles []domain.IndexedFile, task domain.Task, allRules []domain.Rule, graphAvailable bool) domain.ContextPack {
	analysis := AnalyzeTask(task)
	caps := DetectRepoCapabilities(repoRoot, metaFiles)
	ruleGroups := MatchRulesV2(allRules, analysis)
	matchedRules := append(append(append(ruleGroups.Architecture, ruleGroups.Contracts...), ruleGroups.Security...), ruleGroups.Testing...)
	candidates := RankFileCandidates(ctx, repoRoot, metaFiles, analysis, caps, matchedRules)
	if analysis.MainCapability != "" && analysis.MainCapability != "unknown" && !capabilityExists(analysis.MainCapability, caps) {
		candidates = nil
	}
	quality := ComputeContextQuality(analysis, caps, candidates, ruleGroups, graphAvailable)
	risks := BuildRisks(analysis)
	tests := SuggestTests(analysis)
	return domain.ContextPack{
		TaskID: task.ID, GeneratedAt: time.Now().UTC(), AgentBudget: domain.AgentBudget{OpenTopFilesFirst: min(2, len(candidates)), ExplorationMode: "minimal", TokenMode: BudgetCavernicola},
		TaskSummary: summarize(task.Content), TaskAnalysis: analysis, ContextQuality: quality, RepositoryCapabilities: caps,
		DetectedTopics: topicNames(analysis.TechnicalTopics), DetectedTechnicalTopics: analysis.TechnicalTopics,
		RelevantArchitectureRules: ruleGroups.Architecture, RelevantBusinessTechnicalRules: append(append(ruleGroups.Contracts, ruleGroups.Security...), ruleGroups.Testing...),
		RelevantRules: ruleGroups, LikelyAffectedLayers: analysis.AffectedLayers, LikelyRelevantFiles: candidates,
		PublicContractImpact: analysis.ContractImpact, Risks: risks, SecurityRisks: risks.Security, ConcurrencyRisks: risks.Concurrency,
		MemoryPerformanceRisks: risks.MemoryPerformance, SuggestedTests: tests, RecommendedStrategy: RecommendedStrategy(analysis),
		KnownPitfalls: knownPitfalls(analysis, caps),
		RecommendedAgentInstructions: []string{
			"Open only the top N files first.",
			"If context quality is low, ask before broad repository exploration.",
			"Do not modify public contracts without approval.",
			"Add/adjust regression tests before finalizing.",
			"Do not run commit, push, merge, reset, or destructive commands.",
		},
	}
}

func normalizeText(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "n plus one", "n+1")
	s = regexp.MustCompile(`(?m)^#+\s*`).ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

func normalizeTopic(w string) string {
	w = strings.Trim(w, ".,:;()[]{}\"'")
	switch w {
	case "relationships":
		return "relationship"
	case "relations":
		return "relation"
	case "resolvers":
		return "resolver"
	case "items":
		return "item"
	case "events":
		return "event"
	case "tests":
		return "test"
	}
	return w
}

func detectTaskType(id, text string) domain.TaskType {
	idText := strings.ToLower(id + " " + text)
	switch {
	case strings.Contains(idText, "bug") || containsAny(idText, " error ", " fails", " null", " incorrect", " broken", " regression", " defect"):
		return domain.TaskTypeBug
	case containsAny(idText, " auth", " authorization", " token", " secret", " credential", " permission", " tenant isolation"):
		return domain.TaskTypeSecurity
	case containsAny(idText, " performance", " slow", " latency", " memory", " n+1", " optimization"):
		return domain.TaskTypePerformance
	case containsAny(idText, " refactor", " cleanup", " restructure"):
		return domain.TaskTypeRefactor
	case containsAny(idText, " test", " coverage"):
		return domain.TaskTypeTest
	case containsAny(idText, " docs", " documentation", " readme"):
		return domain.TaskTypeDocs
	case containsAny(idText, " feature", " implement", " add ", " support", " enable", " new capability"):
		return domain.TaskTypeFeature
	default:
		return domain.TaskTypeUnknown
	}
}

func detectCapabilities(text string, topics []domain.TechnicalTopic) []string {
	hay := text + " " + strings.Join(topicNames(topics), " ")
	var out []string
	checks := map[string][]string{
		"graphql":       {"graphql", "resolver", "schema", "nested count", "totalcount"},
		"rest":          {"rest", "http", "handler", "router"},
		"grpc":          {"grpc", "protobuf", "proto"},
		"events":        {"event", "consumer", "producer", "idempotency", "kafka"},
		"persistence":   {"database", "postgres", "mysql", "sqlite", "repository", "store"},
		"cache":         {"cache"},
		"auth":          {"auth", "token"},
		"authorization": {"authorization", "permission", "tenant isolation"},
		"concurrency":   {"concurrency", "race", "goroutine"},
		"memory":        {"memory", "memory leak"},
		"testing":       {"test", "regression test"},
	}
	order := []string{"graphql", "rest", "grpc", "events", "persistence", "cache", "auth", "authorization", "concurrency", "memory", "testing"}
	for _, cap := range order {
		for _, needle := range checks[cap] {
			if strings.Contains(hay, needle) {
				out = append(out, cap)
				break
			}
		}
	}
	if len(out) == 0 {
		return []string{"unknown"}
	}
	return out
}

func detectContractImpact(caps []string, text string) []string {
	var out []string
	if containsString(caps, "graphql") {
		out = append(out, "GraphQL")
	}
	if containsString(caps, "rest") {
		out = append(out, "REST")
	}
	if containsString(caps, "grpc") {
		out = append(out, "gRPC")
	}
	if containsString(caps, "events") {
		out = append(out, "Events")
	}
	if containsString(caps, "persistence") || strings.Contains(text, "db") || strings.Contains(text, "database") {
		out = append(out, "DB")
	}
	if len(out) == 0 {
		return []string{"None/Unknown"}
	}
	return uniqueStrings(out)
}

func affectedLayers(caps []string) []string {
	var out []string
	if containsString(caps, "graphql") {
		out = append(out, "adapter_graphql", "application", "domain")
	}
	if containsString(caps, "rest") {
		out = append(out, "adapter_rest", "application", "domain")
	}
	if containsString(caps, "grpc") {
		out = append(out, "adapter_grpc", "application", "domain")
	}
	if containsString(caps, "events") {
		out = append(out, "adapter_events", "application", "domain")
	}
	if containsString(caps, "persistence") {
		out = append(out, "adapter_persistence", "application")
	}
	if len(out) == 0 {
		out = append(out, "unknown")
	}
	return uniqueStrings(out)
}

func symbolText(f domain.IndexedFile) string {
	var parts []string
	for _, s := range f.Structs {
		parts = append(parts, s.Name)
	}
	for _, s := range f.Interfaces {
		parts = append(parts, s.Name)
	}
	for _, s := range f.Functions {
		parts = append(parts, s.Name)
	}
	for _, s := range f.Methods {
		parts = append(parts, s.Receiver, s.Name)
	}
	for _, s := range f.Tests {
		parts = append(parts, s.Name)
	}
	for _, c := range f.Contracts {
		parts = append(parts, c.Kind, c.Name, c.Operation, c.Evidence)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func hasContractKind(f domain.IndexedFile, kind string) bool {
	for _, c := range f.Contracts {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

func hasContractPrefix(f domain.IndexedFile, prefix string) bool {
	for _, c := range f.Contracts {
		if strings.HasPrefix(c.Name, prefix) {
			return true
		}
	}
	return false
}

func fileHasContractImpact(f domain.IndexedFile, impact string) bool {
	for _, c := range f.Contracts {
		switch impact {
		case "GraphQL":
			if c.Kind == "GraphQLField" || strings.HasPrefix(c.Name, "GraphQL") {
				return true
			}
		case "REST":
			if c.Kind == "RESTEndpoint" {
				return true
			}
		case "gRPC":
			if c.Kind == "GRPCMethod" || strings.HasPrefix(c.Name, "GRPCService.") {
				return true
			}
		case "Events":
			if c.Kind == "EventType" {
				return true
			}
		case "DB":
			if strings.HasPrefix(c.Name, "DB.") {
				return true
			}
		}
	}
	return false
}

func limitedContentContains(repoRoot, rel string, topics []domain.TechnicalTopic) bool {
	data, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		return false
	}
	if len(data) > 32*1024 {
		data = data[:32*1024]
	}
	hay := strings.ToLower(string(data))
	for _, t := range topics {
		if strings.Contains(t.Name, " ") && strings.Contains(hay, t.Name) {
			return true
		}
	}
	return false
}

func knownPitfalls(analysis domain.TaskAnalysis, caps domain.RepoCapabilities) []string {
	var out []string
	if analysis.MainCapability == "graphql" && !caps.HasGraphQL {
		out = append(out, "Task mentions GraphQL, but no strong GraphQL implementation was detected in this repository. Relevant files may be unavailable or this repo may be the tooling repo, not the target service.")
	}
	if analysis.MainCapability == "graphql" {
		out = append(out, "Do not change schema without approval.", "Do not confuse returned item count with total matching count.")
	}
	return append(out, "Keep adapters thin.", "Do not include secrets or full sensitive file contents in context.")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func capabilityExists(cap string, caps domain.RepoCapabilities) bool {
	switch cap {
	case "graphql":
		return caps.HasGraphQL
	case "rest":
		return caps.HasREST
	case "grpc":
		return caps.HasGRPC
	case "events":
		return caps.HasEvents
	case "persistence":
		return caps.HasPersistence
	case "testing":
		return caps.HasTests
	default:
		return cap == "" || cap == "unknown"
	}
}

func displayCapabilityWarning(cap string) string {
	if cap == "graphql" {
		return "GraphQL task detected, but this repository does not appear to contain GraphQL application code."
	}
	return "Task domain " + cap + " was detected, but matching repository capability was not found."
}

func containsCapability(hay, cap string) bool {
	switch cap {
	case "graphql":
		return strings.Contains(hay, "graphql") || strings.Contains(hay, "resolver") || strings.Contains(hay, "gqlgen") || strings.Contains(hay, "schema")
	case "rest":
		return strings.Contains(hay, "http") || strings.Contains(hay, "rest") || strings.Contains(hay, "handler") || strings.Contains(hay, "router")
	case "grpc":
		return strings.Contains(hay, "grpc") || strings.Contains(hay, "proto")
	case "events":
		return strings.Contains(hay, "event") || strings.Contains(hay, "consumer") || strings.Contains(hay, "producer") || strings.Contains(hay, "kafka")
	case "persistence":
		return strings.Contains(hay, "repository") || strings.Contains(hay, "store") || strings.Contains(hay, "postgres") || strings.Contains(hay, "sqlite")
	default:
		return false
	}
}

func contractMatchesPath(impact, path string) bool {
	switch impact {
	case "GraphQL":
		return strings.Contains(path, "graphql") || strings.Contains(path, "graph") || strings.Contains(path, "resolver") || strings.Contains(path, "schema")
	case "REST":
		return strings.Contains(path, "http") || strings.Contains(path, "rest") || strings.Contains(path, "handler") || strings.Contains(path, "router")
	case "gRPC":
		return strings.Contains(path, "grpc") || strings.Contains(path, "proto")
	case "Events":
		return strings.Contains(path, "event") || strings.Contains(path, "consumer") || strings.Contains(path, "producer") || strings.Contains(path, "kafka")
	case "DB":
		return strings.Contains(path, "postgres") || strings.Contains(path, "mysql") || strings.Contains(path, "sqlite") || strings.Contains(path, "repository")
	default:
		return false
	}
}

func isAgentBrainTooling(path string) bool {
	return strings.HasPrefix(path, "cmd/agent-brain") || strings.Contains(path, "internal/adapters/cli") || strings.Contains(path, "internal/adapters/docker") || strings.Contains(path, "internal/adapters/sqlite") || strings.Contains(path, "internal/adapters/neo4j")
}

func isGenericInfra(path string) bool {
	return strings.HasSuffix(path, "main.go") || strings.Contains(path, "root.go") || strings.Contains(path, "runtime.go") || strings.Contains(path, "store.go")
}

func isRelatedTest(f domain.IndexedFile, existing []domain.FileCandidate) bool {
	if !strings.HasSuffix(strings.ToLower(f.Path), "_test.go") {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(f.Path), "_test.go")
	for _, c := range existing {
		if c.Category == "primary_candidate" && strings.Contains(filepath.Base(c.Path), base) {
			return true
		}
	}
	return false
}

func testMatchesTask(f domain.IndexedFile, analysis domain.TaskAnalysis) bool {
	hay := filepath.ToSlash(strings.ToLower(f.Path)) + " " + symbolText(f)
	if containsCapability(hay, analysis.MainCapability) {
		return true
	}
	for _, topic := range analysis.TechnicalTopics {
		parts := strings.Fields(topic.Name)
		for _, part := range parts {
			if len(part) >= 4 && strings.Contains(hay, part) {
				return true
			}
		}
	}
	return false
}

func candidateReason(c domain.FileCandidate) string {
	if c.Category == "primary_candidate" {
		return "Strong capability/topic/layer match for the task."
	}
	if c.Category == "related_test" {
		return "Related test candidate for implementation file."
	}
	if c.Category == "repo_tooling" {
		return "Repository tooling file; inspect only if task is about agent-brain itself."
	}
	return "Supporting evidence matched, but confidence is limited."
}

func confidence(score int) float64 {
	switch {
	case score >= 180:
		return 0.95
	case score >= 140:
		return 0.85
	case score >= 100:
		return 0.7
	case score >= 70:
		return 0.55
	default:
		return 0.35
	}
}

func ruleCapabilities(rs []domain.Rule) []string {
	var out []string
	for _, r := range rs {
		for _, t := range r.Topics {
			out = appendUnique(out, strings.ToLower(t))
		}
	}
	return out
}

func appendRule(rs []domain.Rule, r domain.Rule) []domain.Rule {
	for _, existing := range rs {
		if existing.ID == r.ID {
			return rs
		}
	}
	return append(rs, r)
}

func topicNames(topics []domain.TechnicalTopic) []string {
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		out = append(out, t.Name)
	}
	return out
}

func firstOrUnknown(xs []string) string {
	if len(xs) == 0 {
		return "unknown"
	}
	return xs[0]
}

func hasTopic(a domain.TaskAnalysis, topic string) bool {
	for _, t := range a.TechnicalTopics {
		if t.Name == topic || strings.Contains(t.Name, topic) {
			return true
		}
	}
	return false
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func containsString(xs []string, x string) bool {
	for _, item := range xs {
		if item == x {
			return true
		}
	}
	return false
}

func uniqueStrings(xs []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, x := range xs {
		if x == "" {
			continue
		}
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}

func appendUnique(xs []string, x string) []string {
	if x == "" {
		return xs
	}
	for _, item := range xs {
		if item == x {
			return xs
		}
	}
	return append(xs, x)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
