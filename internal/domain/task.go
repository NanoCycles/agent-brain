package domain

type Task struct {
	ID      string
	Path    string
	Title   string
	Content string
	Topics  []string
}

type TaskType string

const (
	TaskTypeBug         TaskType = "bug"
	TaskTypeFeature     TaskType = "feature"
	TaskTypeRefactor    TaskType = "refactor"
	TaskTypeSecurity    TaskType = "security"
	TaskTypePerformance TaskType = "performance"
	TaskTypeTest        TaskType = "test"
	TaskTypeDocs        TaskType = "docs"
	TaskTypeUnknown     TaskType = "unknown"
)

type TechnicalTopic struct {
	Name  string `json:"name" yaml:"name"`
	Score int    `json:"score" yaml:"score"`
}

type TaskAnalysis struct {
	Type             TaskType         `json:"type" yaml:"type"`
	MainCapability   string           `json:"main_capability" yaml:"main_capability"`
	Capabilities     []string         `json:"capabilities" yaml:"capabilities"`
	ContractImpact   []string         `json:"contract_impact" yaml:"contract_impact"`
	AffectedLayers   []string         `json:"affected_layers" yaml:"affected_layers"`
	TechnicalTopics  []TechnicalTopic `json:"technical_topics" yaml:"technical_topics"`
	PrimaryTopicText string           `json:"primary_topic_text" yaml:"primary_topic_text"`
}

type DomainCapability string

const (
	CapabilityGraphQL       DomainCapability = "graphql"
	CapabilityREST          DomainCapability = "rest"
	CapabilityGRPC          DomainCapability = "grpc"
	CapabilityEvents        DomainCapability = "events"
	CapabilityPersistence   DomainCapability = "persistence"
	CapabilityCache         DomainCapability = "cache"
	CapabilityAuth          DomainCapability = "auth"
	CapabilityAuthorization DomainCapability = "authorization"
	CapabilityConcurrency   DomainCapability = "concurrency"
	CapabilityMemory        DomainCapability = "memory"
	CapabilityTesting       DomainCapability = "testing"
)
