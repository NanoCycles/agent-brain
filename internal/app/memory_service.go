package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
	"gopkg.in/yaml.v3"
)

type MemoryService struct {
	meta  ports.MetadataStore
	graph ports.GraphStore
}

func NewMemoryService(meta ports.MetadataStore, graph ...ports.GraphStore) *MemoryService {
	var g ports.GraphStore
	if len(graph) > 0 {
		g = graph[0]
	}
	return &MemoryService{meta: meta, graph: g}
}

type MemoryProposal struct {
	TaskID         string   `yaml:"task_id"`
	GeneratedAt    string   `yaml:"generated_at"`
	LessonsLearned []string `yaml:"lessons_learned"`
	SuggestedRules []string `yaml:"suggested_rules"`
	RelatedBugs    []string `yaml:"bugs_related"`
	TestsAdded     []string `yaml:"tests_added"`
	FilesModified  []string `yaml:"files_modified"`
	RisksDetected  []string `yaml:"risks_detected"`
}

func (s *MemoryService) GenerateProposal(ctx context.Context, repoRoot, taskPath, outputDir string) (string, error) {
	task, err := ReadTask(taskPath)
	if err != nil {
		return "", err
	}
	files := gitChangedFiles(ctx, repoRoot)
	var tests []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			tests = append(tests, f)
		}
	}
	prop := MemoryProposal{
		TaskID: task.ID, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		LessonsLearned: []string{"Fill after implementation review."},
		SuggestedRules: []string{"Fill only if a reusable technical/business rule emerged."},
		RelatedBugs:    []string{}, TestsAdded: tests, FilesModified: files,
		RisksDetected: []string{"Review public contracts, security, concurrency, and persistence impact before applying memory."},
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outputDir, task.ID+".yml")
	data, err := yaml.Marshal(prop)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	if s.meta != nil {
		_ = s.meta.SaveMemoryProposal(ctx, task.ID, path)
	}
	return path, nil
}

func (s *MemoryService) ApplyProposal(ctx context.Context, repoRoot, path, outputDir string, confirmed bool) (string, error) {
	if !confirmed {
		return "", os.ErrPermission
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var prop MemoryProposal
	if err := yaml.Unmarshal(data, &prop); err != nil {
		return "", err
	}
	if prop.TaskID == "" {
		return "", os.ErrInvalid
	}
	appliedDir := filepath.Join(outputDir, "applied")
	if err := os.MkdirAll(appliedDir, 0o755); err != nil {
		return "", err
	}
	appliedPath := filepath.Join(appliedDir, prop.TaskID+".yml")
	if err := os.WriteFile(appliedPath, data, 0o644); err != nil {
		return "", err
	}
	record := domain.MemoryRecord{
		TaskID: prop.TaskID, RepoRoot: repoRoot, AppliedAt: time.Now().UTC(), SourcePath: path,
		LessonsLearned: prop.LessonsLearned, SuggestedRules: prop.SuggestedRules, RelatedBugs: prop.RelatedBugs,
		TestsAdded: prop.TestsAdded, FilesModified: prop.FilesModified, RisksDetected: prop.RisksDetected,
	}
	if s.meta != nil {
		_ = s.meta.SaveAppliedMemory(ctx, record)
	}
	if s.graph != nil {
		_ = s.graph.SaveMemory(ctx, record)
	}
	return appliedPath, nil
}

func gitChangedFiles(ctx context.Context, root string) []string {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-only")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			files = append(files, strings.TrimSpace(line))
		}
	}
	return files
}
