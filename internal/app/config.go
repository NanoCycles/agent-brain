package app

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ProjectName      string   `yaml:"project_name"`
	RepoRoot         string   `yaml:"repo_root"`
	Neo4jURI         string   `yaml:"neo4j_uri"`
	Neo4jUser        string   `yaml:"neo4j_user"`
	Neo4jPassword    string   `yaml:"neo4j_password"`
	SQLitePath       string   `yaml:"sqlite_path"`
	ContextOutputDir string   `yaml:"context_output_dir"`
	RulesDir         string   `yaml:"rules_dir"`
	IgnorePatterns   []string `yaml:"ignore_patterns"`
}

func DefaultConfig(repoRoot string) Config {
	return Config{
		ProjectName:      "agent-brain",
		RepoRoot:         repoRoot,
		Neo4jURI:         "bolt://localhost:7687",
		Neo4jUser:        "neo4j",
		Neo4jPassword:    "agentbrain",
		SQLitePath:       ".agent-brain/runtime/metadata.sqlite",
		ContextOutputDir: ".ai/context",
		RulesDir:         ".agent-brain/rules",
		IgnorePatterns: []string{
			".git", "vendor", "node_modules", "dist", "build", "target", "bin", "coverage",
			".env", ".env.*", "*.pem", "*.key", "credentials", "secrets", "kubeconfig", "id_rsa", "id_ed25519",
		},
	}
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) YAML() ([]byte, error) {
	return yaml.Marshal(c)
}
