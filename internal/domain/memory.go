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

type DomainMemory struct {
	GeneratedAt      time.Time           `json:"generated_at" yaml:"generated_at"`
	DomainConcepts   []DomainConcept     `json:"domain_concepts" yaml:"domain_concepts"`
	SystemComponents []SystemComponent   `json:"system_components" yaml:"system_components"`
	BusinessRules    []BusinessRule      `json:"business_rules" yaml:"business_rules"`
	Invariants       []BusinessInvariant `json:"invariants" yaml:"invariants"`
}

type DomainConcept struct {
	Name          string   `json:"name" yaml:"name"`
	Area          string   `json:"area" yaml:"area"`
	Description   string   `json:"description" yaml:"description"`
	Evidence      []string `json:"evidence" yaml:"evidence"`
	ImplementedBy []string `json:"implemented_by" yaml:"implemented_by"`
	Confidence    float64  `json:"confidence" yaml:"confidence"`
	OpenQuestions []string `json:"open_questions,omitempty" yaml:"open_questions,omitempty"`
}

type SystemComponent struct {
	Name           string   `json:"name" yaml:"name"`
	Area           string   `json:"area" yaml:"area"`
	Kind           string   `json:"kind" yaml:"kind"`
	Responsibility string   `json:"responsibility" yaml:"responsibility"`
	Files          []string `json:"files" yaml:"files"`
	Contracts      []string `json:"contracts" yaml:"contracts"`
	Confidence     float64  `json:"confidence" yaml:"confidence"`
}

type BusinessRule struct {
	Name       string   `json:"name" yaml:"name"`
	Area       string   `json:"area" yaml:"area"`
	Statement  string   `json:"statement" yaml:"statement"`
	Evidence   []string `json:"evidence" yaml:"evidence"`
	Confidence float64  `json:"confidence" yaml:"confidence"`
}

type BusinessInvariant struct {
	Name       string   `json:"name" yaml:"name"`
	Area       string   `json:"area" yaml:"area"`
	Statement  string   `json:"statement" yaml:"statement"`
	Evidence   []string `json:"evidence" yaml:"evidence"`
	Confidence float64  `json:"confidence" yaml:"confidence"`
}
