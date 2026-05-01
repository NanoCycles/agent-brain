package domain

import "time"

type Repository struct {
	ID        int64
	Name      string
	Root      string
	GoModule  string
	CommitSHA string
	IndexedAt time.Time
}

type IndexRun struct {
	ID          int64
	RepoRoot    string
	CommitSHA   string
	StartedAt   time.Time
	CompletedAt time.Time
	FileCount   int
	NodeCount   int
	RelCount    int
}

type IndexedFile struct {
	Path       string
	Package    string
	Layer      string
	Hash       string
	IndexedAt  time.Time
	Structs    []Struct
	Interfaces []Interface
	Functions  []Function
	Methods    []Method
	Tests      []Test
	Imports    []string
	Contracts  []Contract
}

type CapabilityEvidence struct {
	Capability string  `json:"capability" yaml:"capability"`
	Path       string  `json:"path" yaml:"path"`
	Reason     string  `json:"reason" yaml:"reason"`
	Confidence float64 `json:"confidence" yaml:"confidence"`
}

type RepoCapabilities struct {
	HasGraphQL     bool                 `json:"has_graphql" yaml:"has_graphql"`
	HasREST        bool                 `json:"has_rest" yaml:"has_rest"`
	HasGRPC        bool                 `json:"has_grpc" yaml:"has_grpc"`
	HasEvents      bool                 `json:"has_events" yaml:"has_events"`
	HasPersistence bool                 `json:"has_persistence" yaml:"has_persistence"`
	HasTests       bool                 `json:"has_tests" yaml:"has_tests"`
	HasGoMod       bool                 `json:"has_go_mod" yaml:"has_go_mod"`
	MainLanguage   string               `json:"main_language" yaml:"main_language"`
	DetectedLayers []string             `json:"detected_layers" yaml:"detected_layers"`
	Evidence       []CapabilityEvidence `json:"evidence" yaml:"evidence"`
}
