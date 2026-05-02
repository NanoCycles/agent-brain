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
}

type PrepareResult struct {
	Pack         domain.ContextPack
	MarkdownPath string
	JSONPath     string
	Indexed      bool
	RuntimeUp    bool
	Handoff      string
}

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
	if opts.TaskPath == "" && opts.Topic == "" {
		return PrepareResult{}, fmt.Errorf("provide --task or --topic")
	}
	if opts.TaskPath != "" && opts.Topic != "" {
		return PrepareResult{}, fmt.Errorf("use only one of --task or --topic")
	}
	if err := s.initService.Init(p.Root); err != nil {
		return PrepareResult{}, err
	}
	cfg, _ = LoadConfig(p.ConfigPath)
	spec := RuntimeSpecFromConfig(cfg, p.ComposePath)
	result := PrepareResult{}
	if !s.runtime.Neo4jRunning(ctx, spec) {
		if err := s.runtime.Up(ctx, spec); err != nil {
			return PrepareResult{}, err
		}
		result.RuntimeUp = true
	}
	if err := s.meta.Init(ctx); err != nil {
		return PrepareResult{}, err
	}
	shouldIndex := !opts.NoIndex
	if opts.Fast && !opts.NoIndex {
		run, _ := s.meta.LastIndexRun(ctx, p.Root)
		shouldIndex = run == nil || time.Since(run.CompletedAt) > 10*time.Minute
	}
	if shouldIndex {
		if _, err := NewIndexService(s.indexer, s.meta, s.graph).Index(ctx, p.Root); err != nil {
			return PrepareResult{}, err
		}
		result.Indexed = true
	}
	contextService := NewContextService(s.meta, s.graph)
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
