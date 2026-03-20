package emotion

import (
	"context"
	"fmt"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
	pb "github.com/swarm-emotions/orchestrator/pkg/proto/emotion_engine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const traceIDMetadataKey = "x-trace-id"

type Client struct {
	conn   *grpc.ClientConn
	client pb.EmotionEngineServiceClient
}

func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4*1024*1024)),
	)
	if err != nil {
		return nil, fmt.Errorf("connect emotion engine: %w", err)
	}
	return &Client{
		conn:   conn,
		client: pb.NewEmotionEngineServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) TransitionState(ctx context.Context, req *connector.TransitionRequest) (*connector.TransitionResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	callCtx = withTraceMetadata(callCtx)
	resp, err := c.client.TransitionState(callCtx, &pb.TransitionStateRequest{
		CurrentState:   toProtoState(req.CurrentState),
		Stimulus:       req.Stimulus,
		StimulusVector: toProtoVector(req.StimulusVector),
		AgentId:        req.AgentID,
	})
	if err != nil {
		return nil, err
	}
	return &connector.TransitionResponse{
		NewState:           fromProtoState(resp.GetNewState()),
		TransitionOccurred: resp.GetTransitionOccurred(),
		BlockedReason:      resp.GetBlockedReason(),
	}, nil
}

func (c *Client) ComputeEmotionVector(ctx context.Context, req *connector.ComputeRequest) (*connector.ComputeResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	callCtx = withTraceMetadata(callCtx)
	resp, err := c.client.ComputeEmotionVector(callCtx, &pb.ComputeEmotionVectorRequest{
		CurrentEmotion: toProtoVector(req.CurrentEmotion),
		Trigger:        toProtoVector(req.Trigger),
		WMatrix: &pb.SusceptibilityMatrix{
			Values:    append([]float32(nil), req.WMatrix...),
			Dimension: uint32(req.WDimension),
		},
		Baseline:    toProtoVector(req.Baseline),
		DecayLambda: req.DecayLambda,
		DeltaTime:   req.DeltaTime,
		EnableNoise: req.EnableNoise,
		NoiseSigma:  req.NoiseSigma,
	})
	if err != nil {
		return nil, err
	}
	return &connector.ComputeResponse{
		NewEmotion: fromProtoVector(resp.GetNewEmotion()),
		Intensity:  resp.GetIntensity(),
	}, nil
}

func (c *Client) FuseScores(ctx context.Context, req *connector.FuseRequest) (*connector.FuseResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	callCtx = withTraceMetadata(callCtx)
	candidates := make([]*pb.ScoreCandidate, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		candidates = append(candidates, &pb.ScoreCandidate{
			MemoryId:          candidate.MemoryID,
			SemanticScore:     candidate.SemanticScore,
			EmotionalScore:    candidate.EmotionalScore,
			CognitiveScore:    candidate.CognitiveScore,
			MemoryLevel:       candidate.MemoryLevel,
			IsPseudopermanent: candidate.IsPseudopermanent,
		})
	}
	resp, err := c.client.FuseScores(callCtx, &pb.FuseScoresRequest{
		Candidates:      candidates,
		Alpha:           req.Weights.Alpha,
		Beta:            req.Weights.Beta,
		Gamma:           req.Weights.Gamma,
		PseudopermBoost: req.Weights.PseudopermBoost,
		CurrentEmotion:  toProtoVector(req.CurrentEmotion),
	})
	if err != nil {
		return nil, err
	}
	ranked := make([]model.RankedMemory, 0, len(resp.GetRanked()))
	for _, item := range resp.GetRanked() {
		ranked = append(ranked, model.RankedMemory{
			MemoryID:              item.GetMemoryId(),
			FinalScore:            item.GetFinalScore(),
			SemanticContribution:  item.GetSemanticContribution(),
			EmotionalContribution: item.GetEmotionalContribution(),
			CognitiveContribution: item.GetCognitiveContribution(),
		})
	}
	return &connector.FuseResponse{Ranked: ranked}, nil
}

func (c *Client) EvaluatePromotion(ctx context.Context, req *connector.PromotionRequest) (*connector.PromotionResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	callCtx = withTraceMetadata(callCtx)
	memories := make([]*pb.MemoryForPromotion, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		memories = append(memories, &pb.MemoryForPromotion{
			MemoryId:         candidate.MemoryID,
			Intensity:        candidate.Intensity,
			CurrentLevel:     candidate.CurrentLevel,
			AccessFrequency:  candidate.AccessFrequency,
			ValenceMagnitude: candidate.ValenceMagnitude,
		})
	}
	resp, err := c.client.EvaluatePromotion(callCtx, &pb.EvaluatePromotionRequest{
		Memories:           memories,
		IntensityThreshold: req.IntensityThreshold,
		FrequencyThreshold: req.FrequencyThreshold,
		ValenceThreshold:   req.ValenceThreshold,
	})
	if err != nil {
		return nil, err
	}
	decisions := make([]model.PromotionDecision, 0, len(resp.GetDecisions()))
	for _, decision := range resp.GetDecisions() {
		decisions = append(decisions, model.PromotionDecision{
			MemoryID:      decision.GetMemoryId(),
			ShouldPromote: decision.GetShouldPromote(),
			TargetLevel:   decision.GetTargetLevel(),
			Reason:        decision.GetReason(),
		})
	}
	return &connector.PromotionResponse{Decisions: decisions}, nil
}

func (c *Client) ProcessInteraction(ctx context.Context, req *connector.ProcessRequest) (*connector.ProcessResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	callCtx = withTraceMetadata(callCtx)
	scoreCandidates := make([]*pb.ScoreCandidate, 0, len(req.ScoreCandidates))
	for _, candidate := range req.ScoreCandidates {
		scoreCandidates = append(scoreCandidates, &pb.ScoreCandidate{
			MemoryId:          candidate.MemoryID,
			SemanticScore:     candidate.SemanticScore,
			EmotionalScore:    candidate.EmotionalScore,
			CognitiveScore:    candidate.CognitiveScore,
			MemoryLevel:       candidate.MemoryLevel,
			IsPseudopermanent: candidate.IsPseudopermanent,
		})
	}
	promotionCandidates := make([]*pb.MemoryForPromotion, 0, len(req.PromotionCandidates))
	for _, candidate := range req.PromotionCandidates {
		promotionCandidates = append(promotionCandidates, &pb.MemoryForPromotion{
			MemoryId:         candidate.MemoryID,
			Intensity:        candidate.Intensity,
			CurrentLevel:     candidate.CurrentLevel,
			AccessFrequency:  candidate.AccessFrequency,
			ValenceMagnitude: candidate.ValenceMagnitude,
		})
	}
	resp, err := c.client.ProcessInteraction(callCtx, &pb.ProcessInteractionRequest{
		AgentId:             req.AgentID,
		CurrentFsmState:     toProtoState(req.CurrentState),
		CurrentEmotion:      toProtoVector(req.CurrentEmotion),
		Stimulus:            req.Stimulus,
		StimulusVector:      toProtoVector(req.StimulusVector),
		WMatrix:             &pb.SusceptibilityMatrix{Values: req.Config.WMatrix, Dimension: uint32(req.Config.WDimension)},
		Baseline:            toProtoVector(req.Config.Baseline),
		DecayLambda:         req.Config.DecayLambda,
		DeltaTime:           1,
		EnableNoise:         req.Config.NoiseEnabled,
		NoiseSigma:          req.Config.NoiseSigma,
		ScoreCandidates:     scoreCandidates,
		Alpha:               req.Config.Weights.Alpha,
		Beta:                req.Config.Weights.Beta,
		Gamma:               req.Config.Weights.Gamma,
		PseudopermBoost:     req.Config.Weights.PseudopermBoost,
		PromotionCandidates: promotionCandidates,
		IntensityThreshold:  0.9,
		FrequencyThreshold:  10,
		ValenceThreshold:    0.8,
	})
	if err != nil {
		return nil, err
	}
	ranked := make([]model.RankedMemory, 0, len(resp.GetRankedMemories()))
	for _, item := range resp.GetRankedMemories() {
		ranked = append(ranked, model.RankedMemory{
			MemoryID:              item.GetMemoryId(),
			FinalScore:            item.GetFinalScore(),
			SemanticContribution:  item.GetSemanticContribution(),
			EmotionalContribution: item.GetEmotionalContribution(),
			CognitiveContribution: item.GetCognitiveContribution(),
		})
	}
	promotions := make([]model.PromotionDecision, 0, len(resp.GetPromotionDecisions()))
	for _, decision := range resp.GetPromotionDecisions() {
		promotions = append(promotions, model.PromotionDecision{
			MemoryID:      decision.GetMemoryId(),
			ShouldPromote: decision.GetShouldPromote(),
			TargetLevel:   decision.GetTargetLevel(),
			Reason:        decision.GetReason(),
		})
	}
	return &connector.ProcessResponse{
		NewState:           fromProtoState(resp.GetNewFsmState()),
		TransitionOccurred: resp.GetTransitionOccurred(),
		NewEmotion:         fromProtoVector(resp.GetNewEmotion()),
		NewIntensity:       resp.GetNewIntensity(),
		Ranked:             ranked,
		Promotions:         promotions,
	}, nil
}

func toProtoVector(v model.EmotionVector) *pb.EmotionVector {
	return &pb.EmotionVector{Components: append([]float32(nil), v.Components...)}
}

func withTraceMetadata(ctx context.Context) context.Context {
	if traceID := tracectx.TraceID(ctx); traceID != "" {
		if md, ok := metadata.FromOutgoingContext(ctx); ok {
			if values := md.Get(traceIDMetadataKey); len(values) > 0 && values[0] != "" {
				return ctx
			}
		}
		return metadata.AppendToOutgoingContext(ctx, traceIDMetadataKey, traceID)
	}
	return ctx
}

func toProtoState(s model.FsmState) *pb.FsmState {
	return &pb.FsmState{
		StateName:   s.StateName,
		MacroState:  s.MacroState,
		EnteredAtMs: s.EnteredAt,
	}
}

func fromProtoVector(v *pb.EmotionVector) model.EmotionVector {
	if v == nil {
		return model.EmotionVector{}
	}
	return model.EmotionVector{Components: append([]float32(nil), v.GetComponents()...)}
}

func fromProtoState(s *pb.FsmState) model.FsmState {
	if s == nil {
		return model.FsmState{}
	}
	return model.FsmState{
		StateName:  s.GetStateName(),
		MacroState: s.GetMacroState(),
		EnteredAt:  s.GetEnteredAtMs(),
	}
}
