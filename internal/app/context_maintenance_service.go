package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NanoCycles/agent-brain/internal/platform/paths"
)

type CleanContextResult struct {
	Removed []string
	Skipped []string
}

type ContextOutputStats struct {
	GeneratedPacks int
	MarkdownFiles  int
	JSONFiles      int
}

func CountGeneratedContext(p paths.ProjectPaths) ContextOutputStats {
	var stats ContextOutputStats
	for _, dir := range []string{p.AIContextDir, p.ContextDir} {
		md, _ := filepath.Glob(filepath.Join(dir, "*.agent.md"))
		js, _ := filepath.Glob(filepath.Join(dir, "*.agent.json"))
		stats.MarkdownFiles += len(md)
		stats.JSONFiles += len(js)
	}
	if stats.MarkdownFiles > stats.JSONFiles {
		stats.GeneratedPacks = stats.MarkdownFiles
	} else {
		stats.GeneratedPacks = stats.JSONFiles
	}
	return stats
}

func CleanGeneratedContext(p paths.ProjectPaths, confirmed bool) (CleanContextResult, error) {
	if !confirmed {
		return CleanContextResult{}, fmt.Errorf("clean-context requires confirmation; pass --confirm or confirmed=true")
	}
	var result CleanContextResult
	candidates := []string{
		filepath.Join(p.AIContextDir, "*.agent.md"),
		filepath.Join(p.AIContextDir, "*.agent.json"),
		filepath.Join(p.ContextDir, "*.agent.md"),
		filepath.Join(p.ContextDir, "*.agent.json"),
	}
	for _, pattern := range candidates {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return result, err
		}
		for _, path := range matches {
			if !isContextOutputPath(p, path) {
				result.Skipped = append(result.Skipped, path)
				continue
			}
			if err := os.Remove(path); err != nil {
				return result, err
			}
			result.Removed = append(result.Removed, path)
		}
	}
	return result, nil
}

func isContextOutputPath(p paths.ProjectPaths, path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	allowedDirs := []string{p.AIContextDir, p.ContextDir}
	allowedSuffix := strings.HasSuffix(abs, ".agent.md") || strings.HasSuffix(abs, ".agent.json")
	if !allowedSuffix {
		return false
	}
	for _, dir := range allowedDirs {
		dirAbs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(dirAbs, abs)
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return true
		}
	}
	return false
}
