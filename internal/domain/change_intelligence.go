package domain

type ChangeScopeLevel string

const (
	ChangeScopeSmall    ChangeScopeLevel = "small"
	ChangeScopeMedium   ChangeScopeLevel = "medium"
	ChangeScopeLarge    ChangeScopeLevel = "large"
	ChangeScopeCritical ChangeScopeLevel = "critical"
)

type ChangeScope struct {
	Level          ChangeScopeLevel `json:"level" yaml:"level"`
	Score          int              `json:"score" yaml:"score"`
	Reasons        []string         `json:"reasons" yaml:"reasons"`
	RequiredGates  []string         `json:"required_gates" yaml:"required_gates"`
	AgentMode      string           `json:"agent_mode" yaml:"agent_mode"`
	HumanApprovals []string         `json:"human_approvals,omitempty" yaml:"human_approvals,omitempty"`
}

type PlanGateReport struct {
	Decision      ReviewDecision `json:"decision" yaml:"decision"`
	Scope         ChangeScope    `json:"scope" yaml:"scope"`
	Checklist     []string       `json:"checklist" yaml:"checklist"`
	Findings      []Finding      `json:"findings" yaml:"findings"`
	AgentActions  []string       `json:"agent_actions" yaml:"agent_actions"`
	ReviewSummary string         `json:"review_summary" yaml:"review_summary"`
}
