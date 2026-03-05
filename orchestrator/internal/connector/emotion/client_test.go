package emotion

import (
	"context"
	"net"
	"testing"

	"github.com/swarm-emotions/orchestrator/internal/connector"
	"github.com/swarm-emotions/orchestrator/internal/model"
	pb "github.com/swarm-emotions/orchestrator/pkg/proto/emotion_engine/v1"
	"google.golang.org/grpc"
)

type testServer struct {
	pb.UnimplementedEmotionEngineServiceServer
}

func (s *testServer) TransitionState(context.Context, *pb.TransitionStateRequest) (*pb.TransitionStateResponse, error) {
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
	server := grpc.NewServer()
	pb.RegisterEmotionEngineServiceServer(server, &testServer{})
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil {
			t.Logf("grpc test server stopped: %v", serveErr)
		}
	}()
	defer server.Stop()

	client, err := NewClient(listener.Addr().String())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	response, err := client.TransitionState(context.Background(), &connector.TransitionRequest{
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
}
