package domain

import "time"

type ContextPack struct {
	TaskID                         string            `json:"task_id" yaml:"task_id"`
	GeneratedAt                    time.Time         `json:"generated_at" yaml:"generated_at"`
	AgentBudget                    AgentBudget       `json:"agent_budget" yaml:"agent_budget"`
	ContextEfficiency              ContextEfficiency `json:"context_efficiency" yaml:"context_efficiency"`
	TaskSummary                    string            `json:"task_summary" yaml:"task_summary"`
	TaskAnalysis                   TaskAnalysis      `json:"task_analysis" yaml:"task_analysis"`
	ContextQuality                 ContextQuality    `json:"context_quality" yaml:"context_quality"`
	RepositoryCapabilities         RepoCapabilities  `json:"repository_capabilities" yaml:"repository_capabilities"`
	DetectedTopics                 []string          `json:"detected_topics" yaml:"detected_topics"`
	DetectedTechnicalTopics        []TechnicalTopic  `json:"detected_technical_topics" yaml:"detected_technical_topics"`
	RelevantArchitectureRules      []Rule            `json:"relevant_architecture_rules" yaml:"relevant_architecture_rules"`
	RelevantBusinessTechnicalRules []Rule            `json:"relevant_business_technical_rules" yaml:"relevant_business_technical_rules"`
	RelevantRules                  RuleGroups        `json:"relevant_rules" yaml:"relevant_rules"`
	LikelyAffectedLayers           []string          `json:"likely_affected_layers" yaml:"likely_affected_layers"`
	LikelyRelevantFiles            []FileCandidate   `json:"likely_relevant_files" yaml:"likely_relevant_files"`
	PublicContractImpact           []string          `json:"public_contract_impact" yaml:"public_contract_impact"`
	Risks                          RiskReport        `json:"risks" yaml:"risks"`
	SecurityRisks                  []string          `json:"security_risks" yaml:"security_risks"`
	ConcurrencyRisks               []string          `json:"concurrency_risks" yaml:"concurrency_risks"`
	MemoryPerformanceRisks         []string          `json:"memory_performance_risks" yaml:"memory_performance_risks"`
	SuggestedTests                 []string          `json:"suggested_tests" yaml:"suggested_tests"`
	RecommendedStrategy            []string          `json:"recommended_strategy" yaml:"recommended_strategy"`
	ImplementationChecklist        []string          `json:"implementation_checklist" yaml:"implementation_checklist"`
	RelevantSystemMemory           []string          `json:"relevant_system_memory" yaml:"relevant_system_memory"`
	KnownPitfalls                  []string          `json:"known_pitfalls" yaml:"known_pitfalls"`
	RecommendedAgentInstructions   []string          `json:"recommended_agent_instructions" yaml:"recommended_agent_instructions"`
}

type AgentBudget struct {
	OpenTopFilesFirst int    `json:"open_top_files_first" yaml:"open_top_files_first"`
	ExplorationMode   string `json:"exploration_mode" yaml:"exploration_mode"`
	TokenMode         string `json:"token_mode" yaml:"token_mode"`
}

type ContextEfficiency struct {
	IndexedFiles           int    `json:"indexed_files" yaml:"indexed_files"`
	ReturnedFiles          int    `json:"returned_files" yaml:"returned_files"`
	FilesAvoided           int    `json:"files_avoided" yaml:"files_avoided"`
	OpenTopFilesFirst      int    `json:"open_top_files_first" yaml:"open_top_files_first"`
	EstimatedTokensSaved   int    `json:"estimated_tokens_saved" yaml:"estimated_tokens_saved"`
	ExplorationMode        string `json:"exploration_mode" yaml:"exploration_mode"`
	RecommendedAgentAction string `json:"recommended_agent_action" yaml:"recommended_agent_action"`
}

type FileCandidate struct {
	Path                string   `json:"path" yaml:"path"`
	Reason              string   `json:"reason" yaml:"reason"`
	Confidence          float64  `json:"confidence" yaml:"confidence"`
	Score               int      `json:"score" yaml:"score"`
	MatchedTopics       []string `json:"matched_topics" yaml:"matched_topics"`
	MatchedCapabilities []string `json:"matched_capabilities" yaml:"matched_capabilities"`
	Source              string   `json:"source" yaml:"source"`
	Evidence            []string `json:"evidence" yaml:"evidence"`
	Category            string   `json:"category" yaml:"category"`
	Warning             string   `json:"warning,omitempty" yaml:"warning,omitempty"`
}

type ContextQuality struct {
	Score                 float64  `json:"score" yaml:"score"`
	Level                 string   `json:"level" yaml:"level"`
	Reasons               []string `json:"reasons" yaml:"reasons"`
	Warnings              []string `json:"warnings" yaml:"warnings"`
	RecommendedNextAction string   `json:"recommended_next_action" yaml:"recommended_next_action"`
}

type RiskReport struct {
	Security          []string `json:"security" yaml:"security"`
	Concurrency       []string `json:"concurrency" yaml:"concurrency"`
	MemoryPerformance []string `json:"memory_performance" yaml:"memory_performance"`
}

type RuleGroups struct {
	Architecture []Rule `json:"architecture" yaml:"architecture"`
	Contracts    []Rule `json:"contracts" yaml:"contracts"`
	Security     []Rule `json:"security" yaml:"security"`
	Testing      []Rule `json:"testing" yaml:"testing"`
}
