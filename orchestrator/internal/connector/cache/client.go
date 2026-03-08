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
	}
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

func (c *Client) Ready(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	return c.rdb.Ping(probeCtx).Err()
}

func (c *Client) GetAgentState(ctx context.Context, agentID string) (*model.AgentState, error) {
	key := stateKey(agentID)
	raw, err := c.rdb.Get(ctx, key).Bytes()
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
	return c.rdb.Set(ctx, stateKey(agentID), payload, 0).Err()
}

func (c *Client) GetWorkingMemory(ctx context.Context, agentID string) ([]model.WorkingMemoryEntry, error) {
	values, err := c.rdb.ZRevRange(ctx, workingMemoryKey(agentID), 0, -1).Result()
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
}

func (c *Client) PushWorkingMemory(ctx context.Context, agentID string, entry model.WorkingMemoryEntry) error {
	if entry.CreatedAtMs == 0 {
		entry.CreatedAtMs = time.Now().UnixMilli()
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return c.rdb.ZAdd(ctx, workingMemoryKey(agentID), redis.Z{
		Score:  float64(entry.CreatedAtMs),
		Member: string(payload),
	}).Err()
}

func (c *Client) AcquireAgentLock(ctx context.Context, agentID string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	token, err := newLockToken()
	if err != nil {
		return false, err
	}

	locked, err := c.rdb.SetNX(ctx, lockKey(agentID), token, ttl).Result()
	if err != nil {
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
	_, err := releaseLockScript.Run(ctx, c.rdb, []string{lockKey(agentID)}, token).Result()
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
