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
				rule("graphql.no_n_plus_one", "List-returning GraphQL relations must use DataLoader or batched fetchers; resolver-per-row queries are blocked.", "critical", "graphql", "performance", "n+1", "dataloader", "batch"),
				rule("graphql.depth_complexity_limits", "Public GraphQL endpoints must enforce query depth or complexity limits.", "critical", "graphql", "dos", "complexity", "depth"),
				rule("graphql.resolver_authorization", "Resolvers returning non-public data must enforce authorization and tenant/project isolation.", "critical", "graphql", "auth", "authorization", "tenant", "project"),
				rule("graphql.websocket_auth", "GraphQL WebSocket connection_init must validate auth before accepting operations.", "critical", "graphql", "websocket", "auth", "subscription"),
				rule("graphql.subscription_tenant_revalidation", "Subscription messages must re-validate tenant/auth context.", "critical", "graphql", "subscription", "tenant", "auth"),
				rule("graphql.dataloader_context", "DataLoaders must propagate context and respect cancellation/timeouts.", "high", "graphql", "dataloader", "context", "timeout"),
				rule("graphql.subscription_panic_recovery", "Subscription handlers must recover from panics and avoid killing the connection loop.", "high", "graphql", "subscription", "panic"),
				rule("graphql.subscription_rate_limit", "Subscriptions must have per-connection rate limits or backpressure.", "high", "graphql", "subscription", "rate", "limit"),
				rule("graphql.generated_code", "Schema changes must regenerate gqlgen/generated code and update tests.", "high", "graphql", "schema", "generated", "gqlgen"),
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
				rule("security.boundary_validation", "HTTP/GraphQL/queue/RPC boundaries must validate input.", "critical", "security", "validation", "boundary"),
				rule("security.no_sensitive_logging", "Do not log PII, secrets, tokens, or full request/response bodies.", "critical", "security", "logging", "pii", "secret"),
				rule("security.no_injection", "Prevent SQL/NoSQL/command/path injection at all external boundaries.", "critical", "security", "injection", "sql", "nosql", "command", "path"),
				rule("security.cache_tenant_scope", "Cache keys for tenant/project data must include tenant/project scope.", "high", "security", "cache", "tenant", "project"),
			},
		},
		"testing.rules.yml": {
			Name: "testing",
			Rules: []domain.Rule{
				rule("testing.bug_regression", "Every bug requires regression test.", "high", "testing", "bug"),
				rule("testing.functional_unit", "Every functional change requires unit tests.", "medium", "testing"),
				rule("testing.integration_transport", "Integration tests required for persistence/transport behavior.", "medium", "testing", "persistence", "transport"),
				rule("testing.race_detector", "Race detector required for concurrency changes.", "medium", "testing", "concurrency"),
				rule("testing.security_critical_paths", "Security-sensitive code requires negative and boundary tests.", "high", "testing", "security", "auth", "authorization"),
				rule("testing.idempotency_retry", "Retry/idempotency logic requires duplicate, failure, and concurrency tests.", "high", "testing", "events", "idempotency", "retry"),
			},
		},
	}
}

func rule(id, title, severity string, topics ...string) domain.Rule {
	return domain.Rule{ID: id, Title: title, Description: title, Severity: severity, Topics: topics}
}
