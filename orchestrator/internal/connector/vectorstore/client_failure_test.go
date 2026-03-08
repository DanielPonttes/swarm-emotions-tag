package vectorstore

import "testing"

func TestNewClientFailsWhenQdrantUnavailable(t *testing.T) {
	_, err := NewClient("127.0.0.1:65535", "unavailable-test")
	if err == nil {
		t.Fatalf("expected error when qdrant is unavailable")
	}
}
