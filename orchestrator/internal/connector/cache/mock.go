package cache

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

type MockClient struct {
	mu                      sync.RWMutex
	states                  map[string]*model.AgentState
	workingMemory           map[string][]model.WorkingMemoryEntry
	workingMemoryExpiry     map[string]time.Time
	workingMemoryTTL        time.Duration
	workingMemoryMaxEntries int
	locks                   map[string]time.Time
}

func NewMockClient() *MockClient {
	return NewMockClientWithConfig(ClientConfig{})
}

func NewMockClientWithConfig(cfg ClientConfig) *MockClient {
	if cfg.WorkingMemoryTTL <= 0 {
		cfg.WorkingMemoryTTL = defaultWorkingMemoryTTL
	}
	if cfg.WorkingMemoryMaxEntries <= 0 {
		cfg.WorkingMemoryMaxEntries = defaultWorkingMemoryWindow
	}
	return &MockClient{
		states:                  make(map[string]*model.AgentState),
		workingMemory:           make(map[string][]model.WorkingMemoryEntry),
		workingMemoryExpiry:     make(map[string]time.Time),
		workingMemoryTTL:        cfg.WorkingMemoryTTL,
		workingMemoryMaxEntries: cfg.WorkingMemoryMaxEntries,
		locks:                   make(map[string]time.Time),
	}
}

func (c *MockClient) Ready(context.Context) error {
	return nil
}

func (c *MockClient) GetAgentState(_ context.Context, agentID string) (*model.AgentState, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, ok := c.states[agentID]
	if !ok {
		return nil, nil
	}
	cloned := *state
	cloned.CurrentEmotion = state.CurrentEmotion.Clone()
	return &cloned, nil
}

func (c *MockClient) SetAgentState(_ context.Context, agentID string, state *model.AgentState) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := *state
	cloned.AgentID = agentID
	cloned.CurrentEmotion = state.CurrentEmotion.Clone()
	c.states[agentID] = &cloned
	return nil
}

func (c *MockClient) GetWorkingMemory(_ context.Context, agentID string) ([]model.WorkingMemoryEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.clearExpiredWorkingMemoryLocked(agentID, time.Now())

	entries := c.workingMemory[agentID]
	out := make([]model.WorkingMemoryEntry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAtMs > out[j].CreatedAtMs
	})
	return out, nil
}

func (c *MockClient) PushWorkingMemory(_ context.Context, agentID string, entry model.WorkingMemoryEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.clearExpiredWorkingMemoryLocked(agentID, time.Now())
	if entry.CreatedAtMs == 0 {
		entry.CreatedAtMs = time.Now().UnixMilli()
	}
	entries := append(c.workingMemory[agentID], entry)
	c.workingMemory[agentID] = pruneWorkingMemoryEntries(entries, c.workingMemoryMaxEntries)
	c.workingMemoryExpiry[agentID] = time.Now().Add(c.workingMemoryTTL)
	return nil
}

func (c *MockClient) AcquireAgentLock(_ context.Context, agentID string, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if expiresAt, ok := c.locks[agentID]; ok && time.Now().Before(expiresAt) {
		return false, nil
	}
	c.locks[agentID] = time.Now().Add(ttl)
	return true, nil
}

func (c *MockClient) ReleaseAgentLock(_ context.Context, agentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.locks[agentID]; !ok {
		return fmt.Errorf("lock for agent %s not found", agentID)
	}
	delete(c.locks, agentID)
	return nil
}

func pruneWorkingMemoryEntries(entries []model.WorkingMemoryEntry, limit int) []model.WorkingMemoryEntry {
	if limit <= 0 || len(entries) == 0 {
		return nil
	}

	out := make([]model.WorkingMemoryEntry, len(entries))
	copy(out, entries)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAtMs == out[j].CreatedAtMs {
			return out[i].MemoryID > out[j].MemoryID
		}
		return out[i].CreatedAtMs > out[j].CreatedAtMs
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (c *MockClient) workingMemoryExpiredLocked(agentID string, now time.Time) bool {
	expiresAt, ok := c.workingMemoryExpiry[agentID]
	return ok && !now.Before(expiresAt)
}

func (c *MockClient) clearExpiredWorkingMemoryLocked(agentID string, now time.Time) {
	if !c.workingMemoryExpiredLocked(agentID, now) {
		return
	}
	delete(c.workingMemory, agentID)
	delete(c.workingMemoryExpiry, agentID)
}
