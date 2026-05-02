package domain

import "time"

type MemoryRecord struct {
	TaskID         string    `json:"task_id" yaml:"task_id"`
	RepoRoot       string    `json:"repo_root" yaml:"repo_root"`
	AppliedAt      time.Time `json:"applied_at" yaml:"applied_at"`
	LessonsLearned []string  `json:"lessons_learned" yaml:"lessons_learned"`
	SuggestedRules []string  `json:"suggested_rules" yaml:"suggested_rules"`
	RelatedBugs    []string  `json:"bugs_related" yaml:"bugs_related"`
	TestsAdded     []string  `json:"tests_added" yaml:"tests_added"`
	FilesModified  []string  `json:"files_modified" yaml:"files_modified"`
	RisksDetected  []string  `json:"risks_detected" yaml:"risks_detected"`
	SourcePath     string    `json:"source_path" yaml:"source_path"`
}
