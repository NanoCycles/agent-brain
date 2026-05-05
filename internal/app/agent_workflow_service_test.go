package app

import (
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestRenderAgentWorkflowResultIncludesMemoryProposalAndNextActions(t *testing.T) {
	result := AgentWorkflowResult{
		Prepare: PrepareResult{
			MarkdownPath: "C:/repo/.ai/context/AK-1.agent.md",
			JSONPath:     "C:/repo/.ai/context/AK-1.agent.json",
			Pack: domain.ContextPack{
				ContextQuality: domain.ContextQuality{Level: "high", Score: 1},
				ContextEfficiency: domain.ContextEfficiency{
					IndexedFiles:         100,
					ReturnedFiles:        4,
					FilesAvoided:         96,
					EstimatedTokensSaved: 76800,
				},
				LikelyRelevantFiles: []domain.FileCandidate{{Path: "a.go"}},
			},
			Handoff: "Read context first.",
		},
		DomainProposalPath: "C:/repo/.ai/memory-proposals/domain-initial.domain.yml",
		DomainProposal: domain.DomainMemory{
			DomainConcepts:   []domain.DomainConcept{{Name: "API"}},
			SystemComponents: []domain.SystemComponent{{Name: "Adapter"}},
		},
	}
	text := RenderAgentWorkflowResult(result)
	for _, want := range []string{"Agent workflow ready", "Context pack:", "Efficiency:", "Domain memory proposal:", "Human approval required", "review_diff_async"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in workflow result, got %s", want, text)
		}
	}
}
