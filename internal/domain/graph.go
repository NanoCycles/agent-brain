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
	Name string
	Path string
	Line int
}

type Test struct {
	Name string
	Path string
	Line int
}

type CodeIndex struct {
	Repository Repository
	Files      []IndexedFile
	Nodes      []GraphNode
	Relations  []GraphRelationship
}
