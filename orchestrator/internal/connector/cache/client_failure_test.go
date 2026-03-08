package cache

import (
	"context"
	"testing"
)

func TestReadyFailsWhenRedisUnavailable(t *testing.T) {
	client := NewClient("127.0.0.1:6399")
	defer client.Close()

	if err := client.Ready(context.Background()); err == nil {
		t.Fatalf("expected ready check to fail for unavailable redis")
	}
}
