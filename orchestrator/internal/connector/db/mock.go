package db

import (
	"context"
	"sort"
	"sync"

	"github.com/swarm-emotions/orchestrator/internal/model"
)

type MockClient struct {
	mu        sync.RWMutex
	configs   map[string]*model.AgentConfig
	cognitive map[string]*model.CognitiveContext
	logs      map[string][]model.InteractionLog
	history   map[string][]model.EmotionHistoryEntry
}

func NewMockClient() *MockClient {
	return &MockClient{
		configs:   make(map[string]*model.AgentConfig),
		cognitive: make(map[string]*model.CognitiveContext),
		logs:      make(map[string][]model.InteractionLog),
		history:   make(map[string][]model.EmotionHistoryEntry),
	}
}

func (c *MockClient) Ready(context.Context) error {
	return nil
}

func (c *MockClient) GetAgentConfig(_ context.Context, agentID string) (*model.AgentConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cfg, ok := c.configs[agentID]
	if !ok {
		return nil, nil
	}
	cloned := *cfg
	cloned.Baseline = cfg.Baseline.Clone()
	cloned.WMatrix = append([]float32(nil), cfg.WMatrix...)
	return &cloned, nil
}

func (c *MockClient) SaveAgentConfig(_ context.Context, cfg *model.AgentConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := *cfg
	cloned.Baseline = cfg.Baseline.Clone()
	cloned.WMatrix = append([]float32(nil), cfg.WMatrix...)
	c.configs[cfg.AgentID] = &cloned
	return nil
}

func (c *MockClient) ListAgentConfigs(context.Context) ([]model.AgentConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]string, 0, len(c.configs))
	for id := range c.configs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]model.AgentConfig, 0, len(ids))
	for _, id := range ids {
		cfg := *c.configs[id]
		cfg.Baseline = c.configs[id].Baseline.Clone()
		cfg.WMatrix = append([]float32(nil), c.configs[id].WMatrix...)
		out = append(out, cfg)
	}
	return out, nil
}

func (c *MockClient) DeleteAgentConfig(_ context.Context, agentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.configs, agentID)
	delete(c.cognitive, agentID)
	delete(c.logs, agentID)
	delete(c.history, agentID)
	return nil
}

func (c *MockClient) GetCognitiveContext(_ context.Context, agentID string) (*model.CognitiveContext, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ctx, ok := c.cognitive[agentID]
	if !ok {
		return model.DefaultCognitiveContext(agentID), nil
	}
	return ctx.Clone(), nil
}

func (c *MockClient) UpdateCognitiveContext(_ context.Context, agentID string, cognitive *model.CognitiveContext) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := cognitive.Clone()
	cloned.AgentID = agentID
	cloned.Normalize()
	c.cognitive[agentID] = cloned
	return nil
}

func (c *MockClient) LogInteraction(_ context.Context, entry *model.InteractionLog) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs[entry.AgentID] = append(c.logs[entry.AgentID], *entry)
	return nil
}

func (c *MockClient) GetInteractionLogs(_ context.Context, agentID string) ([]model.InteractionLog, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.InteractionLog, len(c.logs[agentID]))
	copy(out, c.logs[agentID])
	return out, nil
}

func (c *MockClient) AppendEmotionHistory(_ context.Context, entry *model.EmotionHistoryEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history[entry.AgentID] = append(c.history[entry.AgentID], *entry)
	return nil
}

func (c *MockClient) GetEmotionHistory(_ context.Context, agentID string) ([]model.EmotionHistoryEntry, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.EmotionHistoryEntry, len(c.history[agentID]))
	copy(out, c.history[agentID])
	return out, nil
}
