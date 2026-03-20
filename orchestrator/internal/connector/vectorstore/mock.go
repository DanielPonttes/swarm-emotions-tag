package vectorstore

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type MockClient struct {
	mu       sync.RWMutex
	memories map[string][]model.StoredMemory
}

func NewMockClient() *MockClient {
	return &MockClient{
		memories: make(map[string][]model.StoredMemory),
	}
}

func (c *MockClient) QuerySemantic(_ context.Context, params connector.QuerySemanticParams) ([]model.MemoryHit, error) {
	c.mu.RLock()
	stored := append([]model.StoredMemory(nil), c.memories[params.AgentID]...)
	c.mu.RUnlock()

	hits := make([]model.MemoryHit, 0, len(stored)+2)
	queryVector := textEmbedding(params.Text, 6)
	for _, memory := range stored {
		score := cosineSimilarity(queryVector, blendVectors(textEmbedding(memory.Content, 6), ensureDimension(memory.Emotion.Components, 6)))
		hits = append(hits, model.MemoryHit{
			MemoryID:          memory.MemoryID,
			Content:           memory.Content,
			SemanticScore:     score,
			CognitiveScore:    memory.CognitiveScore,
			MemoryLevel:       memory.MemoryLevel,
			IsPseudopermanent: memory.IsPseudopermanent,
			CreatedAtMs:       memory.CreatedAtMs,
		})
	}

	base := float32(0.55)
	if strings.Contains(strings.ToLower(params.Text), "deadline") {
		base = 0.75
	}
	hits = append(hits, []model.MemoryHit{
		{
			MemoryID:       fmt.Sprintf("%s-semantic-1", params.AgentID),
			Content:        "Previous relevant task discussion",
			SemanticScore:  base,
			CognitiveScore: 0.25,
			MemoryLevel:    2,
		},
		{
			MemoryID:       fmt.Sprintf("%s-semantic-2", params.AgentID),
			Content:        "User preference about concise answers",
			SemanticScore:  base - 0.1,
			CognitiveScore: 0.20,
			MemoryLevel:    1,
		},
	}...)
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].SemanticScore > hits[j].SemanticScore
	})
	return hits, nil
}

func (c *MockClient) QueryEmotional(_ context.Context, params connector.QueryEmotionalParams) ([]model.MemoryHit, error) {
	c.mu.RLock()
	stored := append([]model.StoredMemory(nil), c.memories[params.AgentID]...)
	c.mu.RUnlock()

	hits := make([]model.MemoryHit, 0, len(stored)+2)
	queryVector := ensureDimension(params.EmotionVector.Components, 6)
	for _, memory := range stored {
		score := cosineSimilarity(queryVector, blendVectors(textEmbedding(memory.Content, 6), ensureDimension(memory.Emotion.Components, 6)))
		hits = append(hits, model.MemoryHit{
			MemoryID:          memory.MemoryID,
			Content:           memory.Content,
			EmotionalScore:    score,
			CognitiveScore:    memory.CognitiveScore,
			MemoryLevel:       memory.MemoryLevel,
			IsPseudopermanent: memory.IsPseudopermanent,
			CreatedAtMs:       memory.CreatedAtMs,
		})
	}

	intensity := params.EmotionVector.Intensity()
	hits = append(hits, []model.MemoryHit{
		{
			MemoryID:          fmt.Sprintf("%s-emotional-1", params.AgentID),
			Content:           "Memory aligned with current emotional tone",
			EmotionalScore:    minFloat32(0.9, 0.4+intensity*0.2),
			IsPseudopermanent: true,
			MemoryLevel:       2,
		},
		{
			MemoryID:       fmt.Sprintf("%s-emotional-2", params.AgentID),
			Content:        "Secondary emotionally similar memory",
			EmotionalScore: minFloat32(0.8, 0.3+intensity*0.15),
			MemoryLevel:    1,
		},
	}...)
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].EmotionalScore > hits[j].EmotionalScore
	})
	return hits, nil
}

func (c *MockClient) UpsertMemory(_ context.Context, memory model.StoredMemory) error {
	if strings.TrimSpace(memory.MemoryID) == "" {
		return fmt.Errorf("memory_id is required")
	}
	if strings.TrimSpace(memory.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if memory.MemoryLevel == 0 {
		memory.MemoryLevel = 1
	}
	if memory.CreatedAtMs == 0 {
		memory.CreatedAtMs = 1
	}
	if memory.ValenceMagnitude == 0 && len(memory.Emotion.Components) > 0 {
		memory.ValenceMagnitude = float32(math.Abs(float64(memory.Emotion.Components[0])))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	items := c.memories[memory.AgentID]
	replaced := false
	for i := range items {
		if items[i].MemoryID == memory.MemoryID {
			items[i] = memory
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, memory)
	}
	c.memories[memory.AgentID] = items
	return nil
}

func (c *MockClient) GetMemoriesByLevel(_ context.Context, agentID string, level uint32, limit int) ([]model.StoredMemory, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]model.StoredMemory, 0)
	for _, memory := range c.memories[agentID] {
		if memory.MemoryLevel == level {
			out = append(out, memory)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAtMs > out[j].CreatedAtMs
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c *MockClient) UpdateMemoryLevel(_ context.Context, memoryID string, level uint32) error {
	if strings.TrimSpace(memoryID) == "" {
		return fmt.Errorf("memory_id is required")
	}
	if level == 0 {
		return fmt.Errorf("memory level is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for agentID, items := range c.memories {
		for i := range items {
			if items[i].MemoryID != memoryID {
				continue
			}
			items[i].MemoryLevel = level
			items[i].IsPseudopermanent = level >= 3
			c.memories[agentID] = items
			return nil
		}
	}
	return fmt.Errorf("memory %s not found", memoryID)
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func cosineSimilarity(a, b []float32) float32 {
	size := len(a)
	if len(b) > size {
		size = len(b)
	}
	if size == 0 {
		return 0
	}

	var (
		dot   float32
		normA float32
		normB float32
	)
	for i := 0; i < size; i++ {
		var av, bv float32
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(normA)*float64(normB)))
}
