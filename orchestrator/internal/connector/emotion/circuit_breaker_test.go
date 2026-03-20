package emotion

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	delegate := &flakyEmotionClient{
		err: status.Error(codes.Unavailable, "engine unavailable"),
	}
	breaker := NewCircuitBreakerClient(delegate, CircuitBreakerConfig{
		FailureThreshold: 2,
		OpenTimeout:      200 * time.Millisecond,
	}, nil)

	_, err := breaker.TransitionState(context.Background(), &connector.TransitionRequest{})
	if err == nil {
		t.Fatalf("expected first call to fail")
	}
	if !connector.IsDependencyUnavailable(err) {
		t.Fatalf("expected dependency unavailable on first call, got %v", err)
	}

	_, err = breaker.TransitionState(context.Background(), &connector.TransitionRequest{})
	if err == nil {
		t.Fatalf("expected second call to fail")
	}
	if !connector.IsDependencyUnavailable(err) {
		t.Fatalf("expected dependency unavailable on second call, got %v", err)
	}

	_, err = breaker.TransitionState(context.Background(), &connector.TransitionRequest{})
	if err == nil {
		t.Fatalf("expected third call to fail with open circuit")
	}
	if !connector.IsDependencyUnavailable(err) {
		t.Fatalf("expected dependency unavailable on open circuit, got %v", err)
	}
}

func TestCircuitBreakerHalfOpenRecovery(t *testing.T) {
	delegate := &flakyEmotionClient{
		err: status.Error(codes.Unavailable, "engine unavailable"),
	}
	breaker := NewCircuitBreakerClient(delegate, CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenTimeout:      60 * time.Millisecond,
	}, nil)

	_, _ = breaker.TransitionState(context.Background(), &connector.TransitionRequest{})

	delegate.err = nil
	time.Sleep(80 * time.Millisecond)

	resp, err := breaker.TransitionState(context.Background(), &connector.TransitionRequest{})
	if err != nil {
		t.Fatalf("expected breaker to recover: %v", err)
	}
	if resp == nil || resp.NewState.StateName != "neutral" {
		t.Fatalf("unexpected transition response: %#v", resp)
	}
}

type flakyEmotionClient struct {
	err error
}

func (f *flakyEmotionClient) TransitionState(context.Context, *connector.TransitionRequest) (*connector.TransitionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &connector.TransitionResponse{
		NewState: model.FsmState{
			StateName:  "neutral",
			MacroState: "neutral",
			EnteredAt:  time.Now().UnixMilli(),
		},
		TransitionOccurred: false,
	}, nil
}

func (f *flakyEmotionClient) ComputeEmotionVector(context.Context, *connector.ComputeRequest) (*connector.ComputeResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &connector.ComputeResponse{NewEmotion: model.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}}}, nil
}

func (f *flakyEmotionClient) FuseScores(context.Context, *connector.FuseRequest) (*connector.FuseResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &connector.FuseResponse{}, nil
}

func (f *flakyEmotionClient) EvaluatePromotion(context.Context, *connector.PromotionRequest) (*connector.PromotionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &connector.PromotionResponse{}, nil
}

func (f *flakyEmotionClient) ProcessInteraction(context.Context, *connector.ProcessRequest) (*connector.ProcessResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &connector.ProcessResponse{}, nil
}

func TestIsDependencyUnavailableError(t *testing.T) {
	if isDependencyUnavailableError(context.Canceled) {
		t.Fatalf("context canceled must not be treated as dependency unavailable")
	}
	if !isDependencyUnavailableError(context.DeadlineExceeded) {
		t.Fatalf("deadline exceeded must be treated as dependency unavailable")
	}
	if !isDependencyUnavailableError(errors.New("dial tcp timeout")) {
		t.Fatalf("generic network-like errors should be treated as dependency unavailable")
	}

	transientStatuses := []error{
		status.Error(codes.Unavailable, "engine unavailable"),
		status.Error(codes.DeadlineExceeded, "deadline exceeded"),
		status.Error(codes.ResourceExhausted, "quota exceeded"),
		status.Error(codes.Internal, "internal error"),
	}
	for _, err := range transientStatuses {
		if !isDependencyUnavailableError(err) {
			t.Fatalf("expected transient status to be dependency unavailable: %v", err)
		}
	}

	nonTransientStatuses := []error{
		status.Error(codes.InvalidArgument, "bad request"),
		status.Error(codes.PermissionDenied, "forbidden"),
		status.Error(codes.Unauthenticated, "unauthenticated"),
		status.Error(codes.NotFound, "not found"),
	}
	for _, err := range nonTransientStatuses {
		if isDependencyUnavailableError(err) {
			t.Fatalf("expected non-transient status to remain caller-visible: %v", err)
		}
	}
}
