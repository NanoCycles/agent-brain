package app

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ProjectName      string   `yaml:"project_name"`
	ProjectID        string   `yaml:"project_id"`
	RepoRoot         string   `yaml:"repo_root"`
	RuntimeNamespace string   `yaml:"runtime_namespace"`
	Neo4jURI         string   `yaml:"neo4j_uri"`
	Neo4jHTTPPort    int      `yaml:"neo4j_http_port"`
	Neo4jBoltPort    int      `yaml:"neo4j_bolt_port"`
	Neo4jUser        string   `yaml:"neo4j_user"`
	Neo4jPassword    string   `yaml:"neo4j_password"`
	SQLitePath       string   `yaml:"sqlite_path"`
	ContextOutputDir string   `yaml:"context_output_dir"`
	RulesDir         string   `yaml:"rules_dir"`
	IgnorePatterns   []string `yaml:"ignore_patterns"`
}

func DefaultConfig(repoRoot string) Config {
	projectName := filepath.Base(repoRoot)
	projectID := ProjectID(repoRoot)
	httpPort, boltPort := ProjectPorts(projectID)
	return Config{
		ProjectName:      projectName,
		ProjectID:        projectID,
		RepoRoot:         repoRoot,
		RuntimeNamespace: projectID,
		Neo4jURI:         fmt.Sprintf("bolt://localhost:%d", boltPort),
		Neo4jHTTPPort:    httpPort,
		Neo4jBoltPort:    boltPort,
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
	cfg = NormalizeConfig(cfg)
	return cfg, nil
}

func (c Config) YAML() ([]byte, error) {
	return yaml.Marshal(c)
}

func NormalizeConfig(c Config) Config {
	if c.RepoRoot == "" {
		c.RepoRoot = "."
	}
	if c.ProjectName == "" {
		c.ProjectName = filepath.Base(c.RepoRoot)
	}
	if c.ProjectID == "" {
		c.ProjectID = ProjectID(c.RepoRoot)
	}
	if c.RuntimeNamespace == "" {
		c.RuntimeNamespace = c.ProjectID
	}
	if c.Neo4jBoltPort == 0 {
		c.Neo4jBoltPort = portFromURI(c.Neo4jURI, 7687)
	}
	if c.Neo4jHTTPPort == 0 {
		httpPort, _ := ProjectPorts(c.ProjectID)
		if c.Neo4jBoltPort == 7687 {
			c.Neo4jHTTPPort = 7474
		} else {
			c.Neo4jHTTPPort = httpPort
		}
	}
	if c.Neo4jURI == "" {
		c.Neo4jURI = fmt.Sprintf("bolt://localhost:%d", c.Neo4jBoltPort)
	}
	if c.Neo4jUser == "" {
		c.Neo4jUser = "neo4j"
	}
	if c.Neo4jPassword == "" {
		c.Neo4jPassword = "agentbrain"
	}
	if c.SQLitePath == "" {
		c.SQLitePath = ".agent-brain/runtime/metadata.sqlite"
	}
	if c.ContextOutputDir == "" {
		c.ContextOutputDir = ".ai/context"
	}
	if c.RulesDir == "" {
		c.RulesDir = ".agent-brain/rules"
	}
	return c
}

func ProjectID(repoRoot string) string {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		abs = repoRoot
	}
	base := strings.ToLower(filepath.Base(abs))
	base = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "repo"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(abs)))
	return fmt.Sprintf("%s-%08x", base, h.Sum32())
}

func ProjectPorts(projectID string) (httpPort, boltPort int) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(projectID))
	offset := int(h.Sum32() % 1000)
	return 17474 + offset, 17687 + offset
}

func portFromURI(uri string, fallback int) int {
	if uri == "" {
		return fallback
	}
	idx := strings.LastIndex(uri, ":")
	if idx == -1 || idx == len(uri)-1 {
		return fallback
	}
	port, err := strconv.Atoi(strings.TrimRight(uri[idx+1:], "/"))
	if err != nil {
		return fallback
	}
	return port
}
