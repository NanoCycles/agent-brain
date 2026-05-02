package app

import (
	"strings"
	"testing"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestRenderDomainMemoryFiltersRelevantTopic(t *testing.T) {
	memory := domain.DomainMemory{
		GeneratedAt: time.Now(),
		DomainConcepts: []domain.DomainConcept{
			{Name: "GraphQL API", Area: "graphql", Description: "Exposes nested relationship fields.", Evidence: []string{"graphql/relationships.go"}, Confidence: 0.8},
			{Name: "Billing", Area: "billing", Description: "Handles invoices.", Evidence: []string{"billing.go"}, Confidence: 0.8},
		},
		BusinessRules: []domain.BusinessRule{
			{Name: "Nested relationship count semantics", Area: "graphql", Statement: "Nested relationship counts must not become null.", Evidence: []string{"nested_args_batch_test.go"}, Confidence: 0.8},
		},
	}

	got := RenderDomainMemory(memory, "graphql nested count", "graphql", 8)

	if !strings.Contains(got, "GraphQL API") {
		t.Fatalf("expected GraphQL memory, got %s", got)
	}
	if !strings.Contains(got, "Nested relationship count semantics") {
		t.Fatalf("expected nested count rule, got %s", got)
	}
	if strings.Contains(got, "Billing") {
		t.Fatalf("unexpected unrelated memory, got %s", got)
	}
}
