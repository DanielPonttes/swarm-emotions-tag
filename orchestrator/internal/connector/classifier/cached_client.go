package classifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/observability"
)

const classifierCacheKeyPrefix = "emotion_classifier_cache:"

type classificationCache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Close() error
}

type redisClassificationCache struct {
	client *redis.Client
}

func newRedisClassificationCache(addr string) *redisClassificationCache {
	return &redisClassificationCache{
		client: redis.NewClient(&redis.Options{
			Addr:         addr,
			DialTimeout:  250 * time.Millisecond,
			ReadTimeout:  150 * time.Millisecond,
			WriteTimeout: 150 * time.Millisecond,
			PoolTimeout:  500 * time.Millisecond,
		}),
	}
}

func (c *redisClassificationCache) Get(ctx context.Context, key string) ([]byte, error) {
	return c.client.Get(ctx, key).Bytes()
}

func (c *redisClassificationCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *redisClassificationCache) Close() error {
	return c.client.Close()
}

type CachedClient struct {
	inner   connector.ClassifierClient
	cache   classificationCache
	ttl     time.Duration
	metrics observability.Reporter
}

func NewCachedClient(inner connector.ClassifierClient, redisAddr string, ttl time.Duration) *CachedClient {
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &CachedClient{
		inner:   inner,
		cache:   newRedisClassificationCache(redisAddr),
		ttl:     ttl,
		metrics: observability.NewNoopReporter(),
	}
}

func (c *CachedClient) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		c.metrics = observability.NewNoopReporter()
		return
	}
	c.metrics = reporter
}

func (c *CachedClient) Ready(ctx context.Context) error {
	return c.inner.Ready(ctx)
}

func (c *CachedClient) Close() error {
	if c.cache == nil {
		return nil
	}
	return c.cache.Close()
}

func (c *CachedClient) ClassifyEmotion(ctx context.Context, text string) (*connector.EmotionClassification, error) {
	cacheKey := classifierCacheKey(text)
	if c.cache != nil {
		cached, err := c.cache.Get(ctx, cacheKey)
		if err == nil {
			var result connector.EmotionClassification
			if json.Unmarshal(cached, &result) == nil {
				return &result, nil
			}
		} else {
			c.metrics.IncDependencyError("redis", "classifier_cache_get")
		}
	}

	result, err := c.inner.ClassifyEmotion(ctx, text)
	if err != nil {
		return nil, err
	}

	if c.cache != nil {
		payload, marshalErr := json.Marshal(result)
		if marshalErr == nil {
			if setErr := c.cache.Set(ctx, cacheKey, payload, c.ttl); setErr != nil {
				c.metrics.IncDependencyError("redis", "classifier_cache_set")
			}
		}
	}

	return result, nil
}

func classifierCacheKey(text string) string {
	hash := sha256.Sum256([]byte(text))
	return classifierCacheKeyPrefix + hex.EncodeToString(hash[:])
}
