package connector

import (
	"context"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

type ReadyChecker interface {
	Ready(ctx context.Context) error
}

type TransitionRequest struct {
	AgentID        string
	CurrentState   model.FsmState
	Stimulus       string
	StimulusVector model.EmotionVector
}

type TransitionResponse struct {
	NewState           model.FsmState
	TransitionOccurred bool
	BlockedReason      string
}

type ComputeRequest struct {
	CurrentEmotion model.EmotionVector
	Trigger        model.EmotionVector
	Baseline       model.EmotionVector
	WMatrix        []float32
	WDimension     int
	DecayLambda    float32
	DeltaTime      float32
	EnableNoise    bool
	NoiseSigma     float32
}

type ComputeResponse struct {
	NewEmotion model.EmotionVector
	Intensity  float32
}

type FuseRequest struct {
	Candidates     []model.ScoreCandidate
	Weights        model.ScoreWeights
	CurrentEmotion model.EmotionVector
}

type FuseResponse struct {
	Ranked []model.RankedMemory
}

type PromotionRequest struct {
	Candidates         []model.PromotionCandidate
	IntensityThreshold float32
	FrequencyThreshold uint32
	ValenceThreshold   float32
}

type PromotionResponse struct {
	Decisions []model.PromotionDecision
}

type ProcessRequest struct {
	AgentID             string
	CurrentState        model.FsmState
	CurrentEmotion      model.EmotionVector
	Stimulus            string
	StimulusVector      model.EmotionVector
	Config              *model.AgentConfig
	ScoreCandidates     []model.ScoreCandidate
	PromotionCandidates []model.PromotionCandidate
}

type ProcessResponse struct {
	NewState           model.FsmState
	TransitionOccurred bool
	NewEmotion         model.EmotionVector
	NewIntensity       float32
	Ranked             []model.RankedMemory
	Promotions         []model.PromotionDecision
}

type QuerySemanticParams struct {
	AgentID string
	Text    string
	TopK    int
}

type QueryEmotionalParams struct {
	AgentID       string
	EmotionVector model.EmotionVector
	TopK          int
}

type GenerateOpts struct {
	Model           string
	SystemPrompt    string
	MaxTokens       int
	Temperature     float32
	TopP            float32
	TopK            int
	PresencePenalty float32
	EnableThinking  bool
}

type EmotionClassification struct {
	Vector     model.EmotionVector
	Label      string
	Confidence float32
	Stimulus   string
}

type EmotionEngineClient interface {
	TransitionState(ctx context.Context, req *TransitionRequest) (*TransitionResponse, error)
	ComputeEmotionVector(ctx context.Context, req *ComputeRequest) (*ComputeResponse, error)
	FuseScores(ctx context.Context, req *FuseRequest) (*FuseResponse, error)
	EvaluatePromotion(ctx context.Context, req *PromotionRequest) (*PromotionResponse, error)
	ProcessInteraction(ctx context.Context, req *ProcessRequest) (*ProcessResponse, error)
}

type VectorStoreClient interface {
	QuerySemantic(ctx context.Context, params QuerySemanticParams) ([]model.MemoryHit, error)
	QueryEmotional(ctx context.Context, params QueryEmotionalParams) ([]model.MemoryHit, error)
}

type CacheClient interface {
	Ready(ctx context.Context) error
	GetAgentState(ctx context.Context, agentID string) (*model.AgentState, error)
	SetAgentState(ctx context.Context, agentID string, state *model.AgentState) error
	GetWorkingMemory(ctx context.Context, agentID string) ([]model.WorkingMemoryEntry, error)
	PushWorkingMemory(ctx context.Context, agentID string, entry model.WorkingMemoryEntry) error
	AcquireAgentLock(ctx context.Context, agentID string, ttl time.Duration) (bool, error)
	ReleaseAgentLock(ctx context.Context, agentID string) error
}

type DBClient interface {
	Ready(ctx context.Context) error
	GetAgentConfig(ctx context.Context, agentID string) (*model.AgentConfig, error)
	SaveAgentConfig(ctx context.Context, cfg *model.AgentConfig) error
	ListAgentConfigs(ctx context.Context) ([]model.AgentConfig, error)
	DeleteAgentConfig(ctx context.Context, agentID string) error
	GetCognitiveContext(ctx context.Context, agentID string) (*model.CognitiveContext, error)
	UpdateCognitiveContext(ctx context.Context, agentID string, cognitive *model.CognitiveContext) error
	LogInteraction(ctx context.Context, entry *model.InteractionLog) error
	GetInteractionLogs(ctx context.Context, agentID string) ([]model.InteractionLog, error)
	AppendEmotionHistory(ctx context.Context, entry *model.EmotionHistoryEntry) error
	GetEmotionHistory(ctx context.Context, agentID string) ([]model.EmotionHistoryEntry, error)
}

type LLMProvider interface {
	Ready(ctx context.Context) error
	Generate(ctx context.Context, prompt string, opts GenerateOpts) (string, error)
}

type ClassifierClient interface {
	Ready(ctx context.Context) error
	ClassifyEmotion(ctx context.Context, text string) (*EmotionClassification, error)
}
