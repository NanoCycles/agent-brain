package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/platform/paths"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type AgentWorkflowOptions struct {
	TaskPath        string
	Topic           string
	Budget          string
	Fast            bool
	NoIndex         bool
	BootstrapMemory bool
	MemoryArea      string
}

type AgentWorkflowResult struct {
	Prepare              PrepareResult
	DomainProposalPath   string
	DomainProposal       domain.DomainMemory
	DomainProposalNeeded bool
}

type AgentWorkflowService struct {
	prepare *PrepareService
	meta    ports.MetadataStore
	graph   ports.GraphStore
}

func NewAgentWorkflowService(prepare *PrepareService, meta ports.MetadataStore, graph ports.GraphStore) *AgentWorkflowService {
	return &AgentWorkflowService{prepare: prepare, meta: meta, graph: graph}
}

func (s *AgentWorkflowService) Start(ctx context.Context, p paths.ProjectPaths, cfg Config, opts AgentWorkflowOptions) (AgentWorkflowResult, error) {
	result := AgentWorkflowResult{}
	prep, err := s.prepare.Prepare(ctx, p, cfg, PrepareOptions{
		TaskPath: opts.TaskPath,
		Topic:    opts.Topic,
		Fast:     opts.Fast,
		NoIndex:  opts.NoIndex,
		Budget:   opts.Budget,
	})
	if err != nil {
		return result, err
	}
	result.Prepare = prep
	if opts.BootstrapMemory || domainMemoryAppearsEmpty(ctx, s.meta, p) {
		taskPath := opts.TaskPath
		if taskPath == "" {
			taskPath = prep.Pack.TaskID
		}
		path, memory, err := NewDomainMemoryService(s.meta, s.graph).Propose(ctx, p.Root, taskPathIfExists(taskPath), p.AIMemoryProposals, opts.MemoryArea)
		if err != nil {
			return result, err
		}
		result.DomainProposalPath = path
		result.DomainProposal = memory
		result.DomainProposalNeeded = true
	}
	return result, nil
}

func RenderAgentWorkflowResult(r AgentWorkflowResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Agent workflow ready\n")
	fmt.Fprintf(&b, "Context pack: %s\nJSON: %s\n", r.Prepare.MarkdownPath, r.Prepare.JSONPath)
	fmt.Fprintf(&b, "Quality: %s %.2f\n", r.Prepare.Pack.ContextQuality.Level, r.Prepare.Pack.ContextQuality.Score)
	fmt.Fprintf(&b, "Efficiency: returned %d/%d files; avoided %d; est tokens saved %d\n",
		r.Prepare.Pack.ContextEfficiency.ReturnedFiles,
		r.Prepare.Pack.ContextEfficiency.IndexedFiles,
		r.Prepare.Pack.ContextEfficiency.FilesAvoided,
		r.Prepare.Pack.ContextEfficiency.EstimatedTokensSaved)
	fmt.Fprintf(&b, "Indexed: %t\nRuntime started: %t\nTop files: %d\n", r.Prepare.Indexed, r.Prepare.RuntimeUp, len(r.Prepare.Pack.LikelyRelevantFiles))
	if r.DomainProposalPath != "" {
		fmt.Fprintf(&b, "Domain memory proposal: %s\n", r.DomainProposalPath)
		fmt.Fprintf(&b, "Memory candidates: concepts=%d components=%d rules=%d invariants=%d\n",
			len(r.DomainProposal.DomainConcepts), len(r.DomainProposal.SystemComponents), len(r.DomainProposal.BusinessRules), len(r.DomainProposal.Invariants))
		fmt.Fprintf(&b, "Human approval required before applying domain memory.\n")
	}
	fmt.Fprintf(&b, "\nSuggested prompt:\n%s\n", r.Prepare.Handoff)
	fmt.Fprintf(&b, "\nNext agent actions:\n- Read the context pack first.\n- Open only top-ranked files.\n- Use impact for focused follow-up.\n- Run focused tests.\n- Call review_diff_async before final response.\n- Call finish_task_async after validation.\n")
	return b.String()
}

func domainMemoryAppearsEmpty(ctx context.Context, meta ports.MetadataStore, p paths.ProjectPaths) bool {
	if meta == nil {
		return true
	}
	memory, err := meta.DomainMemory(ctx, p.Root)
	if err != nil {
		return true
	}
	return len(memory.DomainConcepts)+len(memory.SystemComponents)+len(memory.BusinessRules)+len(memory.Invariants) == 0
}

func taskPathIfExists(path string) string {
	if strings.TrimSpace(path) == "" || !strings.HasSuffix(strings.ToLower(path), ".md") {
		return ""
	}
	return path
}
