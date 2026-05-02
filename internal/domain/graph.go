package domain

import "time"

type GraphNode struct {
	Label      string
	Name       string
	Path       string
	Repo       string
	Package    string
	Layer      string
	CommitSHA  string
	IndexedAt  time.Time
	Source     string
	Confidence float64
	Properties map[string]any
}

type GraphRelationship struct {
	FromLabel string
	FromKey   string
	ToLabel   string
	ToKey     string
	Type      string
	Repo      string
	Props     map[string]any
}

type GraphStats struct {
	Nodes         int64
	Relationships int64
}

type Function struct {
	Name string
	Path string
	Line int
}

type Method struct {
	Receiver string
	Name     string
	Path     string
	Line     int
}

type Struct struct {
	Name string
	Path string
	Line int
}

type Interface struct {
	Name    string
	Path    string
	Line    int
	Methods []string
}

type Test struct {
	Name string
	Path string
	Line int
}

type Contract struct {
	Kind       string  `json:"kind" yaml:"kind"`
	Name       string  `json:"name" yaml:"name"`
	Path       string  `json:"path" yaml:"path"`
	Operation  string  `json:"operation" yaml:"operation"`
	Evidence   string  `json:"evidence" yaml:"evidence"`
	Confidence float64 `json:"confidence" yaml:"confidence"`
}

type Call struct {
	CallerKind string `json:"caller_kind" yaml:"caller_kind"`
	CallerKey  string `json:"caller_key" yaml:"caller_key"`
	Callee     string `json:"callee" yaml:"callee"`
}

type CodeIndex struct {
	Repository Repository
	Files      []IndexedFile
	Nodes      []GraphNode
	Relations  []GraphRelationship
}
