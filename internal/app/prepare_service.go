package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/platform/paths"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type PrepareOptions struct {
	TaskPath string
	Topic    string
	Fast     bool
	NoIndex  bool
	Budget   string
	Progress ProgressFunc
}

type PrepareResult struct {
	Pack         domain.ContextPack
	MarkdownPath string
	JSONPath     string
	Indexed      bool
	RuntimeUp    bool
	Handoff      string
}

type ProgressFunc func(progress int, stage string)

type PrepareService struct {
	initService *InitService
	runtime     ports.RuntimeManager
	indexer     ports.CodeIndexer
	meta        ports.MetadataStore
	graph       ports.GraphStore
}

func NewPrepareService(initService *InitService, runtime ports.RuntimeManager, indexer ports.CodeIndexer, meta ports.MetadataStore, graph ports.GraphStore) *PrepareService {
	return &PrepareService{initService: initService, runtime: runtime, indexer: indexer, meta: meta, graph: graph}
}

func (s *PrepareService) Prepare(ctx context.Context, p paths.ProjectPaths, cfg Config, opts PrepareOptions) (PrepareResult, error) {
	report := opts.Progress
	if report == nil {
		report = func(int, string) {}
	}
	if opts.TaskPath == "" && opts.Topic == "" {
		return PrepareResult{}, fmt.Errorf("provide --task or --topic")
	}
	if opts.TaskPath != "" && opts.Topic != "" {
		return PrepareResult{}, fmt.Errorf("use only one of --task or --topic")
	}
	report(5, "initializing project workspace")
	if err := s.initService.Init(p.Root); err != nil {
		return PrepareResult{}, err
	}
	cfg, _ = LoadConfig(p.ConfigPath)
	spec := RuntimeSpecFromConfig(cfg, p.ComposePath)
	result := PrepareResult{}
	report(15, "checking local Neo4j runtime")
	if !s.runtime.Neo4jRunning(ctx, spec) {
		report(20, "starting local Neo4j runtime")
		if err := s.runtime.Up(ctx, spec); err != nil {
			return PrepareResult{}, err
		}
		result.RuntimeUp = true
	}
	report(30, "opening SQLite metadata")
	if err := s.meta.Init(ctx); err != nil {
		return PrepareResult{}, err
	}
	shouldIndex := !opts.NoIndex
	if opts.Fast && !opts.NoIndex {
		run, _ := s.meta.LastIndexRun(ctx, p.Root)
		shouldIndex = run == nil || time.Since(run.CompletedAt) > 10*time.Minute
	}
	if shouldIndex {
		report(45, "indexing repository incrementally")
		if _, err := NewIndexService(s.indexer, s.meta, s.graph).IndexWithOptions(ctx, p.Root, IndexOptions{Incremental: true}); err != nil {
			return PrepareResult{}, err
		}
		result.Indexed = true
		report(70, "repository index ready")
	} else {
		report(55, "using existing repository index")
	}
	contextService := NewContextServiceWithBudget(s.meta, s.graph, opts.Budget)
	report(80, "generating context pack")
	if opts.TaskPath != "" {
		pack, md, js, err := contextService.Generate(ctx, p.Root, opts.TaskPath, p.RulesDir, p.AIContextDir)
		if err != nil {
			return PrepareResult{}, err
		}
		result.Pack, result.MarkdownPath, result.JSONPath = pack, md, js
	} else {
		taskPath, err := writeTopicTask(p, opts.Topic)
		if err != nil {
			return PrepareResult{}, err
		}
		pack, md, js, err := contextService.Generate(ctx, p.Root, taskPath, p.RulesDir, p.AIContextDir)
		if err != nil {
			return PrepareResult{}, err
		}
		result.Pack, result.MarkdownPath, result.JSONPath = pack, md, js
	}
	result.Handoff = HandoffPrompt(result.MarkdownPath)
	report(95, "context pack generated")
	return result, nil
}

func writeTopicTask(p paths.ProjectPaths, topic string) (string, error) {
	taskID := TaskIDFromPath(topic + ".md")
	if taskID == "task" {
		taskID = "topic"
	}
	taskDir := filepath.Join(p.Root, ".ai", "tasks")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(taskDir, taskID+".md")
	data := []byte("# " + taskID + "\n\n" + topic + "\n")
	if err := (filesystem.LocalFS{}).WriteFileIfMissing(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func HandoffPrompt(markdownPath string) string {
	return fmt.Sprintf("Read %s first. Use it as your implementation context and constraints. Open only the top ranked files first, preserve public contracts unless explicitly approved, add or adjust regression tests, and do not run commit, push, merge, reset, or destructive commands.", markdownPath)
}
