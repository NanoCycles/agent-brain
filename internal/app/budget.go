package app

import "github.com/NanoCycles/agent-brain/internal/domain"

const (
	BudgetCavernicola = "cavernicola"
	BudgetCompact     = "compact"
	BudgetStandard    = "standard"
	BudgetDeep        = "deep"
)

func NormalizeBudget(mode string) string {
	switch mode {
	case BudgetCompact, BudgetStandard, BudgetDeep:
		return mode
	case "cavernícola":
		return BudgetCavernicola
	default:
		return BudgetCavernicola
	}
}

func ApplyBudget(p *domain.ContextPack, mode string) {
	mode = NormalizeBudget(mode)
	p.AgentBudget.TokenMode = mode
	switch mode {
	case BudgetDeep:
		p.AgentBudget.OpenTopFilesFirst = min(5, len(p.LikelyRelevantFiles))
	case BudgetStandard:
		p.AgentBudget.OpenTopFilesFirst = min(4, len(p.LikelyRelevantFiles))
		trimPack(p, 8, 12, 8, 8)
	case BudgetCompact:
		p.AgentBudget.OpenTopFilesFirst = min(3, len(p.LikelyRelevantFiles))
		trimPack(p, 6, 10, 6, 6)
	default:
		p.AgentBudget.OpenTopFilesFirst = min(2, len(p.LikelyRelevantFiles))
		p.AgentBudget.ExplorationMode = "minimal"
		trimPack(p, 4, 8, 4, 5)
	}
}

func trimPack(p *domain.ContextPack, files, topics, tests, strategy int) {
	if len(p.LikelyRelevantFiles) > files {
		p.LikelyRelevantFiles = p.LikelyRelevantFiles[:files]
	}
	if len(p.DetectedTopics) > topics {
		p.DetectedTopics = p.DetectedTopics[:topics]
	}
	if len(p.DetectedTechnicalTopics) > topics {
		p.DetectedTechnicalTopics = p.DetectedTechnicalTopics[:topics]
	}
	if len(p.SuggestedTests) > tests {
		p.SuggestedTests = p.SuggestedTests[:tests]
	}
	if len(p.RecommendedStrategy) > strategy {
		p.RecommendedStrategy = p.RecommendedStrategy[:strategy]
	}
	if len(p.RecommendedAgentInstructions) > 4 {
		p.RecommendedAgentInstructions = p.RecommendedAgentInstructions[:4]
	}
}
