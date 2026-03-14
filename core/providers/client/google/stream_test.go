package google

import (
	"context"
	"errors"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestGenAIStreamRecv_ReturnsStreamErrorWhenChannelClosed(t *testing.T) {
	ch := make(chan string)
	close(ch)

	s := &genAIStream{
		ctx:   context.Background(),
		ch:    ch,
		stats: &model.StreamStats{},
	}
	streamErr := errors.New("stream failed")
	s.setStreamError(streamErr)

	_, err := s.Recv()
	if !errors.Is(err, streamErr) {
		t.Fatalf("Recv() error = %v, want %v", err, streamErr)
	}
}
