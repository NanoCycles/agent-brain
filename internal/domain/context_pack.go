package domain

import "time"

type ContextPack struct {
	TaskID                         string    `json:"task_id" yaml:"task_id"`
	GeneratedAt                    time.Time `json:"generated_at" yaml:"generated_at"`
	TaskSummary                    string    `json:"task_summary" yaml:"task_summary"`
	DetectedTopics                 []string  `json:"detected_topics" yaml:"detected_topics"`
	RelevantArchitectureRules      []Rule    `json:"relevant_architecture_rules" yaml:"relevant_architecture_rules"`
	RelevantBusinessTechnicalRules []Rule    `json:"relevant_business_technical_rules" yaml:"relevant_business_technical_rules"`
	LikelyAffectedLayers           []string  `json:"likely_affected_layers" yaml:"likely_affected_layers"`
	LikelyRelevantFiles            []string  `json:"likely_relevant_files" yaml:"likely_relevant_files"`
	PublicContractImpact           []string  `json:"public_contract_impact" yaml:"public_contract_impact"`
	SecurityRisks                  []string  `json:"security_risks" yaml:"security_risks"`
	ConcurrencyRisks               []string  `json:"concurrency_risks" yaml:"concurrency_risks"`
	MemoryPerformanceRisks         []string  `json:"memory_performance_risks" yaml:"memory_performance_risks"`
	SuggestedTests                 []string  `json:"suggested_tests" yaml:"suggested_tests"`
	KnownPitfalls                  []string  `json:"known_pitfalls" yaml:"known_pitfalls"`
	RecommendedAgentInstructions   []string  `json:"recommended_agent_instructions" yaml:"recommended_agent_instructions"`
}
