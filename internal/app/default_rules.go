package app

import "github.com/NanoCycles/agent-brain/internal/domain"

func DefaultRuleSets() map[string]domain.RuleSet {
	return map[string]domain.RuleSet{
		"architecture.rules.yml": {
			Name: "architecture",
			Rules: []domain.Rule{
				rule("architecture.hexagonal", "Hexagonal architecture mandatory.", "high", "architecture", "layers"),
				rule("architecture.domain_pure", "Domain must not depend on frameworks.", "high", "domain", "framework"),
				rule("architecture.usecase_dependencies", "Use cases must not depend on REST/GraphQL/gRPC/DB.", "high", "application", "adapter"),
				rule("architecture.thin_adapters", "Adapters must be thin.", "medium", "adapter", "transport"),
				rule("architecture.small_ports", "Ports must be small and consumer-owned.", "medium", "port", "interface"),
			},
		},
		"graphql.rules.yml": {
			Name: "graphql",
			Rules: []domain.Rule{
				rule("graphql.thin_resolvers", "GraphQL resolvers must be thin.", "medium", "graphql"),
				rule("graphql.schema_approval", "Schema changes require human approval.", "high", "graphql", "schema"),
				rule("graphql.count_semantics", "Nested count must be consistent with top-level count semantics.", "medium", "graphql", "count"),
				rule("graphql.no_n_plus_one", "Avoid N+1 queries.", "high", "graphql", "performance"),
			},
		},
		"grpc.rules.yml": {
			Name: "grpc",
			Rules: []domain.Rule{
				rule("grpc.protobuf_approval", "Protobuf changes require human approval.", "high", "grpc", "protobuf"),
				rule("grpc.thin_services", "gRPC services must be thin.", "medium", "grpc"),
				rule("grpc.error_mapping", "Map errors to proper status codes.", "medium", "grpc", "errors"),
			},
		},
		"events.rules.yml": {
			Name: "events",
			Rules: []domain.Rule{
				rule("events.idempotent_consumers", "Consumers must be idempotent.", "high", "events", "consumer"),
				rule("events.duplicate_delivery", "Consumers must tolerate duplicate delivery.", "high", "events", "consumer"),
				rule("events.no_sensitive_publish", "Producers must not publish sensitive data.", "high", "events", "security"),
				rule("events.contract_approval", "Event contract changes require approval.", "high", "events", "contract"),
			},
		},
		"security.rules.yml": {
			Name: "security",
			Rules: []domain.Rule{
				rule("security.no_secrets_indexing", "No secrets indexing.", "critical", "security", "secret"),
				rule("security.no_env_indexing", "No .env indexing.", "critical", "security", "env"),
				rule("security.no_credential_exposure", "No credential exposure.", "critical", "security", "credential"),
				rule("security.auth_approval", "Auth/authz changes require approval.", "high", "security", "auth"),
				rule("security.tenant_isolation", "Tenant/project isolation must be preserved.", "high", "security", "tenant"),
			},
		},
		"testing.rules.yml": {
			Name: "testing",
			Rules: []domain.Rule{
				rule("testing.bug_regression", "Every bug requires regression test.", "high", "testing", "bug"),
				rule("testing.functional_unit", "Every functional change requires unit tests.", "medium", "testing"),
				rule("testing.integration_transport", "Integration tests required for persistence/transport behavior.", "medium", "testing", "persistence", "transport"),
				rule("testing.race_detector", "Race detector required for concurrency changes.", "medium", "testing", "concurrency"),
			},
		},
	}
}

func rule(id, title, severity string, topics ...string) domain.Rule {
	return domain.Rule{ID: id, Title: title, Description: title, Severity: severity, Topics: topics}
}
