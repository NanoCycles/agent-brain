package domain

type Finding struct {
	Severity string
	Title    string
	Message  string
	Path     string
	RuleID   string
}

type ReviewDecision string

const (
	Approved         ReviewDecision = "APPROVED"
	ChangesRequested ReviewDecision = "CHANGES_REQUESTED"
	Rejected         ReviewDecision = "REJECTED"
)
