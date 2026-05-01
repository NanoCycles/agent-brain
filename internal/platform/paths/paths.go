package paths

import (
	"os"
	"path/filepath"
)

const DirName = ".agent-brain"

type ProjectPaths struct {
	Root              string
	AgentBrainDir     string
	ConfigPath        string
	RulesDir          string
	RuntimeDir        string
	ComposePath       string
	SQLitePath        string
	ContextDir        string
	AIMemoryProposals string
	AIContextDir      string
	LocalMemoryDir    string
}

func Discover(root string) (ProjectPaths, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return ProjectPaths{}, err
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ProjectPaths{}, err
	}
	ab := filepath.Join(abs, DirName)
	return ProjectPaths{
		Root:              abs,
		AgentBrainDir:     ab,
		ConfigPath:        filepath.Join(ab, "config.yml"),
		RulesDir:          filepath.Join(ab, "rules"),
		RuntimeDir:        filepath.Join(ab, "runtime"),
		ComposePath:       filepath.Join(ab, "runtime", "docker-compose.yml"),
		SQLitePath:        filepath.Join(ab, "runtime", "metadata.sqlite"),
		ContextDir:        filepath.Join(ab, "context"),
		AIContextDir:      filepath.Join(abs, ".ai", "context"),
		AIMemoryProposals: filepath.Join(abs, ".ai", "memory-proposals"),
		LocalMemoryDir:    filepath.Join(ab, "memory-proposals"),
	}, nil
}
