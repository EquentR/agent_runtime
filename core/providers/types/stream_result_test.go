package model

import (
	"context"
	"testing"

	"github.com/EquentR/agent_runtime/core/types"
)

type fakeStream struct {
	ctx   context.Context
	stats *StreamStats

	events []StreamEvent
	final  Message
	err    error
}

func (f *fakeStream) Recv() (string, error)            { return "", nil }
func (f *fakeStream) Close() error                     { return nil }
func (f *fakeStream) Context() context.Context         { return f.ctx }
func (f *fakeStream) Stats() *StreamStats              { return f.stats }
func (f *fakeStream) ToolCalls() []types.ToolCall      { return nil }
func (f *fakeStream) ResponseType() StreamResponseType { return StreamResponseUnknown }
func (f *fakeStream) FinishReason() string             { return "" }
func (f *fakeStream) Reasoning() string                { return "" }
func (f *fakeStream) RecvEvent() (StreamEvent, error)  { return f.events[0], nil }
func (f *fakeStream) FinalMessage() (Message, error)   { return f.final, f.err }

func TestStreamFinalMessageContract(t *testing.T) {
	var s Stream = &fakeStream{
		ctx:   context.Background(),
		stats: &StreamStats{},
		final: Message{Role: RoleAssistant, Content: "done"},
	}

	msg, err := s.FinalMessage()
	if err != nil || msg.Content != "done" {
		t.Fatalf("FinalMessage() = %#v, %v", msg, err)
	}
}

func TestStreamRecvEventContract(t *testing.T) {
	var s Stream = &fakeStream{
		ctx:   context.Background(),
		stats: &StreamStats{},
		events: []StreamEvent{{
			Type: StreamEventTextDelta,
			Text: "hello",
		}},
	}

	event, err := s.RecvEvent()
	if err != nil {
		t.Fatalf("RecvEvent() error = %v", err)
	}
	if event.Type != StreamEventTextDelta || event.Text != "hello" {
		t.Fatalf("RecvEvent() = %#v", event)
	}
}
