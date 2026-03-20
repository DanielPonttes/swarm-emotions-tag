package emotion

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/tracectx"
	pb "github.com/swarm-emotions/orchestrator/pkg/proto/emotion_engine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type testServer struct {
	pb.UnimplementedEmotionEngineServiceServer
	metadataCh chan metadata.MD
}

func (s *testServer) TransitionState(ctx context.Context, _ *pb.TransitionStateRequest) (*pb.TransitionStateResponse, error) {
	if s.metadataCh != nil {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			s.metadataCh <- md.Copy()
		}
	}
	return &pb.TransitionStateResponse{
		NewState: &pb.FsmState{
			StateName:   "joyful",
			MacroState:  "positive",
			EnteredAtMs: 123,
		},
		TransitionOccurred: true,
	}, nil
}

func TestClientTransitionState(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runTransitionStateTest(t, listener, listener.Addr().String())
}

func TestClientTransitionStateOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "emotion-engine.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	runTransitionStateTest(t, listener, "unix://"+socketPath)
}

func runTransitionStateTest(t *testing.T, listener net.Listener, addr string) {
	t.Helper()
	server := grpc.NewServer()
	testSvc := &testServer{metadataCh: make(chan metadata.MD, 1)}
	pb.RegisterEmotionEngineServiceServer(server, testSvc)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil {
			t.Logf("grpc test server stopped: %v", serveErr)
		}
	}()
	defer server.Stop()
	defer listener.Close()

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	ctx := tracectx.WithTraceID(context.Background(), "req-123")
	response, err := client.TransitionState(ctx, &connector.TransitionRequest{
		AgentID:      "agent-1",
		CurrentState: model.FsmState{StateName: "neutral", MacroState: "neutral"},
		Stimulus:     "praise",
		StimulusVector: model.EmotionVector{
			Components: []float32{0.8, 0.2, 0.1, 0, 0, 0},
		},
	})
	if err != nil {
		t.Fatalf("transition state: %v", err)
	}
	if response.NewState.StateName != "joyful" {
		t.Fatalf("expected joyful, got %s", response.NewState.StateName)
	}

	select {
	case md := <-testSvc.metadataCh:
		if got := md.Get(traceIDMetadataKey); len(got) != 1 || got[0] != "req-123" {
			t.Fatalf("expected %s metadata to be propagated, got %v", traceIDMetadataKey, got)
		}
	default:
		t.Fatal("expected incoming metadata to be captured")
	}
}
