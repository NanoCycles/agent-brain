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
}
