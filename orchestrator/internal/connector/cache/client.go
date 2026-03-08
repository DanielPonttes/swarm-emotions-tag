package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"github.com/swarm-emotions/orchestrator/internal/resilience"
)

const (
	stateKeyPrefix         = "emotion_state:"
	workingMemoryKeyPrefix = "working_memory:"
	lockKeyPrefix          = "agent_lock:"
)

var releaseLockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

type Client struct {
	rdb *redis.Client

	lockMu     sync.Mutex
	lockTokens map[string]string
	metrics    observability.Reporter
	retry      resilience.RetryPolicy
}

func NewClient(addr string) *Client {
	return &Client{
		rdb: redis.NewClient(&redis.Options{
			Addr:         addr,
			DialTimeout:  250 * time.Millisecond,
			ReadTimeout:  150 * time.Millisecond,
			WriteTimeout: 150 * time.Millisecond,
			PoolTimeout:  500 * time.Millisecond,
		}),
		lockTokens: make(map[string]string),
		metrics:    observability.NewNoopReporter(),
		retry: resilience.RetryPolicy{
			Attempts:  2,
			BaseDelay: 5 * time.Millisecond,
			MaxDelay:  40 * time.Millisecond,
		},
	}
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

func (c *Client) Ready(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	err := c.rdb.Ping(probeCtx).Err()
	if err != nil {
		c.metrics.IncDependencyError("redis", "ready")
	}
	return err
}

func (c *Client) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		c.metrics = observability.NewNoopReporter()
		return
	}
	c.metrics = reporter
}

func (c *Client) GetAgentState(ctx context.Context, agentID string) (*model.AgentState, error) {
	result, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) (*model.AgentState, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 100*time.Millisecond)
		defer cancel()

		raw, err := c.rdb.Get(callCtx, stateKey(agentID)).Bytes()
		if err == redis.Nil {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		var state model.AgentState
		if err := json.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
		state.AgentID = agentID
		return &state, nil
	})
	if err != nil {
		c.metrics.IncDependencyError("redis", "get_agent_state")
	}
	return result, err
}

func (c *Client) SetAgentState(ctx context.Context, agentID string, state *model.AgentState) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}
	cloned := *state
	cloned.AgentID = agentID
	cloned.CurrentEmotion = state.CurrentEmotion.Clone()

	payload, err := json.Marshal(cloned)
	if err != nil {
		return err
	}
	err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 100*time.Millisecond)
		defer cancel()
		return c.rdb.Set(callCtx, stateKey(agentID), payload, 0).Err()
	})
	if err != nil {
		c.metrics.IncDependencyError("redis", "set_agent_state")
	}
	return err
}

func (c *Client) GetWorkingMemory(ctx context.Context, agentID string) ([]model.WorkingMemoryEntry, error) {
	entries, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) ([]model.WorkingMemoryEntry, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 100*time.Millisecond)
		defer cancel()
		values, err := c.rdb.ZRevRange(callCtx, workingMemoryKey(agentID), 0, -1).Result()
		if err != nil {
			return nil, err
		}
		entries := make([]model.WorkingMemoryEntry, 0, len(values))
		for _, value := range values {
			var entry model.WorkingMemoryEntry
			if err := json.Unmarshal([]byte(value), &entry); err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
		return entries, nil
	})
	if err != nil {
		c.metrics.IncDependencyError("redis", "get_working_memory")
	}
	return entries, err
}

func (c *Client) PushWorkingMemory(ctx context.Context, agentID string, entry model.WorkingMemoryEntry) error {
	if entry.CreatedAtMs == 0 {
		entry.CreatedAtMs = time.Now().UnixMilli()
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 100*time.Millisecond)
		defer cancel()
		return c.rdb.ZAdd(callCtx, workingMemoryKey(agentID), redis.Z{
			Score:  float64(entry.CreatedAtMs),
			Member: string(payload),
		}).Err()
	})
	if err != nil {
		c.metrics.IncDependencyError("redis", "push_working_memory")
	}
	return err
}

func (c *Client) AcquireAgentLock(ctx context.Context, agentID string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	token, err := newLockToken()
	if err != nil {
		return false, err
	}

	locked, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) (bool, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 100*time.Millisecond)
		defer cancel()
		return c.rdb.SetNX(callCtx, lockKey(agentID), token, ttl).Result()
	})
	if err != nil {
		c.metrics.IncDependencyError("redis", "acquire_lock")
		return false, err
	}
	if locked {
		c.lockMu.Lock()
		c.lockTokens[agentID] = token
		c.lockMu.Unlock()
	}
	return locked, nil
}

func (c *Client) ReleaseAgentLock(ctx context.Context, agentID string) error {
	token := c.consumeLockToken(agentID)
	if token == "" {
		return nil
	}
	_, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) (any, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 100*time.Millisecond)
		defer cancel()
		return releaseLockScript.Run(callCtx, c.rdb, []string{lockKey(agentID)}, token).Result()
	})
	if err != nil {
		c.metrics.IncDependencyError("redis", "release_lock")
	}
	return err
}

func (c *Client) consumeLockToken(agentID string) string {
	c.lockMu.Lock()
	defer c.lockMu.Unlock()
	token := c.lockTokens[agentID]
	delete(c.lockTokens, agentID)
	return token
}

func stateKey(agentID string) string {
	return stateKeyPrefix + agentID
}

func workingMemoryKey(agentID string) string {
	return workingMemoryKeyPrefix + agentID
}

func lockKey(agentID string) string {
	return lockKeyPrefix + agentID
}

func newLockToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
