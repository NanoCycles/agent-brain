package app

import (
	"fmt"
	"path/filepath"

	"github.com/NanoCycles/agent-brain/internal/platform/paths"
	"github.com/NanoCycles/agent-brain/internal/ports"
	"gopkg.in/yaml.v3"
)

type InitService struct {
	fs ports.FileSystem
}

func NewInitService(fs ports.FileSystem) *InitService {
	return &InitService{fs: fs}
}

func (s *InitService) Init(root string) error {
	p, err := paths.Discover(root)
	if err != nil {
		return err
	}
	dirs := []string{
		p.AgentBrainDir,
		p.RulesDir,
		p.ContextDir,
		p.LocalMemoryDir,
		filepath.Join(p.AgentBrainDir, "snapshots"),
		p.RuntimeDir,
		p.AIContextDir,
		p.AIMemoryProposals,
	}
	for _, dir := range dirs {
		if err := s.fs.EnsureDir(dir); err != nil {
			return err
		}
	}
	for _, keep := range []string{
		filepath.Join(p.ContextDir, ".gitkeep"),
		filepath.Join(p.LocalMemoryDir, ".gitkeep"),
		filepath.Join(p.AgentBrainDir, "snapshots", ".gitkeep"),
		filepath.Join(p.RuntimeDir, ".gitkeep"),
	} {
		if err := s.fs.WriteFileIfMissing(keep, nil, 0o644); err != nil {
			return err
		}
	}
	cfg := DefaultConfig(p.Root)
	data, err := cfg.YAML()
	if err != nil {
		return err
	}
	if err := s.fs.WriteFileIfMissing(p.ConfigPath, data, 0o644); err != nil {
		return err
	}
	for name, rs := range DefaultRuleSets() {
		data, err := yaml.Marshal(rs)
		if err != nil {
			return err
		}
		if err := s.fs.WriteFileIfMissing(filepath.Join(p.RulesDir, name), data, 0o644); err != nil {
			return fmt.Errorf("write rule %s: %w", name, err)
		}
	}
	return nil
}
