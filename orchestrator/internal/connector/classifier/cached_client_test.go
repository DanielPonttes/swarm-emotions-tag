package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

func TestCachedClientUsesCacheAfterFirstClassification(t *testing.T) {
	inner := &stubClassifierClient{
		result: &connector.EmotionClassification{
			Vector:     model.EmotionVector{Components: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}},
			Label:      "gratitude",
			Confidence: 0.9,
			Stimulus:   "praise",
		},
	}
	cache := newMemoryClassificationCache()
	client := &CachedClient{
		inner: inner,
		cache: cache,
		ttl:   time.Hour,
	}

	first, err := client.ClassifyEmotion(context.Background(), "thanks a lot")
	if err != nil {
		t.Fatalf("first classify: %v", err)
	}
	second, err := client.ClassifyEmotion(context.Background(), "thanks a lot")
	if err != nil {
		t.Fatalf("second classify: %v", err)
	}

	if inner.calls != 1 {
		t.Fatalf("expected inner client to be called once, got %d", inner.calls)
	}
	if first.Label != second.Label || second.Label != "gratitude" {
		t.Fatalf("unexpected cached classification: %#v %#v", first, second)
	}
}

func TestFallbackClientReturnsNeutralWhenInnerFails(t *testing.T) {
	client := NewFallbackClient(&stubClassifierClient{err: errors.New("classifier down")})

	result, err := client.ClassifyEmotion(context.Background(), "urgent issue")
	if err != nil {
		t.Fatalf("fallback classify: %v", err)
	}
	if result.Label != "neutral" {
		t.Fatalf("expected neutral label, got %q", result.Label)
	}
	if result.Stimulus != "urgency" {
		t.Fatalf("expected urgency stimulus, got %q", result.Stimulus)
	}
	if len(result.Vector.Components) != 6 {
		t.Fatalf("expected 6-dimensional neutral vector, got %#v", result.Vector.Components)
	}
}

type stubClassifierClient struct {
	result *connector.EmotionClassification
	err    error
	calls  int
}

func (s stubClassifierClient) Ready(context.Context) error {
	return nil
}

func (s *stubClassifierClient) ClassifyEmotion(context.Context, string) (*connector.EmotionClassification, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

type memoryClassificationCache struct {
	values map[string][]byte
}

func newMemoryClassificationCache() *memoryClassificationCache {
	return &memoryClassificationCache{values: make(map[string][]byte)}
}

func (c *memoryClassificationCache) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := c.values[key]
	if !ok {
		return nil, errors.New("cache miss")
	}
	return append([]byte(nil), value...), nil
}

func (c *memoryClassificationCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	c.values[key] = append([]byte(nil), value...)
	return nil
}

func (c *memoryClassificationCache) Close() error {
	return nil
}

func TestCachedClientReadsPreexistingCachedValue(t *testing.T) {
	cache := newMemoryClassificationCache()
	payload, err := json.Marshal(&connector.EmotionClassification{
		Vector:     model.EmotionVector{Components: []float32{0, 0, 0, 0.5, 0, 0}},
		Label:      "neutral",
		Confidence: 0.5,
		Stimulus:   "novelty",
	})
	if err != nil {
		t.Fatalf("marshal cached payload: %v", err)
	}
	cache.values[classifierCacheKey("hello")] = payload

	client := &CachedClient{
		inner: &stubClassifierClient{
			result: &connector.EmotionClassification{
				Vector: model.EmotionVector{Components: []float32{1, 1, 1, 1, 1, 1}},
				Label:  "joy",
			},
		},
		cache: cache,
		ttl:   time.Hour,
	}

	result, err := client.ClassifyEmotion(context.Background(), "hello")
	if err != nil {
		t.Fatalf("classify with cache hit: %v", err)
	}
	if result.Label != "neutral" {
		t.Fatalf("expected cached neutral label, got %q", result.Label)
	}
}
