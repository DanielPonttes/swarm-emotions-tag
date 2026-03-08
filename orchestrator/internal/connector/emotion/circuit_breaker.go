package emotion

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errEmotionCircuitOpen = errors.New("emotion_engine_circuit_open")

type CircuitBreakerConfig struct {
	FailureThreshold int
	OpenTimeout      time.Duration
}

func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		OpenTimeout:      2 * time.Second,
	}
}

type circuitState int

const (
	stateClosed circuitState = iota
	stateOpen
	stateHalfOpen
)

type CircuitBreakerClient struct {
	next    connector.EmotionEngineClient
	config  CircuitBreakerConfig
	metrics observability.Reporter

	mu                sync.Mutex
	state             circuitState
	consecutiveFailed int
	openedUntil       time.Time
	halfOpenInFlight  bool
}

func NewCircuitBreakerClient(
	next connector.EmotionEngineClient,
	config CircuitBreakerConfig,
	metrics observability.Reporter,
) *CircuitBreakerClient {
	if config.FailureThreshold < 1 {
		config.FailureThreshold = 1
	}
	if config.OpenTimeout <= 0 {
		config.OpenTimeout = 2 * time.Second
	}
	if metrics == nil {
		metrics = observability.NewNoopReporter()
	}
	return &CircuitBreakerClient{
		next:    next,
		config:  config,
		metrics: metrics,
		state:   stateClosed,
	}
}

func (c *CircuitBreakerClient) TransitionState(ctx context.Context, req *connector.TransitionRequest) (*connector.TransitionResponse, error) {
	var out *connector.TransitionResponse
	err := c.execute(ctx, "transition_state", func() error {
		var callErr error
		out, callErr = c.next.TransitionState(ctx, req)
		return callErr
	})
	return out, err
}

func (c *CircuitBreakerClient) ComputeEmotionVector(ctx context.Context, req *connector.ComputeRequest) (*connector.ComputeResponse, error) {
	var out *connector.ComputeResponse
	err := c.execute(ctx, "compute_emotion_vector", func() error {
		var callErr error
		out, callErr = c.next.ComputeEmotionVector(ctx, req)
		return callErr
	})
	return out, err
}

func (c *CircuitBreakerClient) FuseScores(ctx context.Context, req *connector.FuseRequest) (*connector.FuseResponse, error) {
	var out *connector.FuseResponse
	err := c.execute(ctx, "fuse_scores", func() error {
		var callErr error
		out, callErr = c.next.FuseScores(ctx, req)
		return callErr
	})
	return out, err
}

func (c *CircuitBreakerClient) EvaluatePromotion(ctx context.Context, req *connector.PromotionRequest) (*connector.PromotionResponse, error) {
	var out *connector.PromotionResponse
	err := c.execute(ctx, "evaluate_promotion", func() error {
		var callErr error
		out, callErr = c.next.EvaluatePromotion(ctx, req)
		return callErr
	})
	return out, err
}

func (c *CircuitBreakerClient) ProcessInteraction(ctx context.Context, req *connector.ProcessRequest) (*connector.ProcessResponse, error) {
	var out *connector.ProcessResponse
	err := c.execute(ctx, "process_interaction", func() error {
		var callErr error
		out, callErr = c.next.ProcessInteraction(ctx, req)
		return callErr
	})
	return out, err
}

func (c *CircuitBreakerClient) execute(ctx context.Context, operation string, fn func() error) error {
	if err := c.beforeCall(); err != nil {
		c.metrics.IncDependencyError("emotion_engine", operation)
		return &connector.DependencyUnavailableError{
			Dependency: "emotion_engine",
			Cause:      err,
		}
	}

	err := fn()
	if err == nil {
		c.afterSuccess()
		return nil
	}

	c.afterFailure()
	if isDependencyUnavailableError(err) {
		c.metrics.IncDependencyError("emotion_engine", operation)
		return &connector.DependencyUnavailableError{
			Dependency: "emotion_engine",
			Cause:      err,
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	c.metrics.IncDependencyError("emotion_engine", operation)
	return err
}

func (c *CircuitBreakerClient) beforeCall() error {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case stateOpen:
		if now.Before(c.openedUntil) {
			return errEmotionCircuitOpen
		}
		c.state = stateHalfOpen
		c.halfOpenInFlight = false
		fallthrough
	case stateHalfOpen:
		if c.halfOpenInFlight {
			return errEmotionCircuitOpen
		}
		c.halfOpenInFlight = true
	case stateClosed:
	}
	return nil
}

func (c *CircuitBreakerClient) afterSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.consecutiveFailed = 0
	if c.state == stateHalfOpen {
		c.halfOpenInFlight = false
		c.state = stateClosed
	}
}

func (c *CircuitBreakerClient) afterFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case stateHalfOpen:
		c.halfOpenInFlight = false
		c.openCircuitLocked()
	case stateOpen:
		c.openCircuitLocked()
	case stateClosed:
		c.consecutiveFailed++
		if c.consecutiveFailed >= c.config.FailureThreshold {
			c.openCircuitLocked()
		}
	}
}

func (c *CircuitBreakerClient) openCircuitLocked() {
	c.state = stateOpen
	c.openedUntil = time.Now().Add(c.config.OpenTimeout)
	c.consecutiveFailed = c.config.FailureThreshold
}

func isDependencyUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return true
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Internal:
		return true
	default:
		return false
	}
}
