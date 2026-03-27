package audit

import coretasks "github.com/EquentR/agent_runtime/core/tasks"

type SchemaVersion string

const (
	SchemaVersionV1 SchemaVersion = "v1"
)

type Phase string

const (
	PhaseRun          Phase = "run"
	PhaseConversation Phase = "conversation"
	PhaseStep         Phase = "step"
	PhasePrompt       Phase = "prompt"
	PhaseRequest      Phase = "request"
	PhaseModel        Phase = "model"
	PhaseTool         Phase = "tool"
	PhaseReplay       Phase = "replay"
)

type Status = coretasks.Status

const (
	StatusQueued          = coretasks.StatusQueued
	StatusRunning         = coretasks.StatusRunning
	StatusWaiting         = coretasks.StatusWaiting
	StatusCancelRequested = coretasks.StatusCancelRequested
	StatusCancelled       = coretasks.StatusCancelled
	StatusSucceeded       = coretasks.StatusSucceeded
	StatusFailed          = coretasks.StatusFailed
)

type ArtifactKind string

const (
	ArtifactKindRequestMessages ArtifactKind = "request_messages"
	ArtifactKindErrorSnapshot   ArtifactKind = "error_snapshot"
	ArtifactKindResolvedPrompt  ArtifactKind = "resolved_prompt"
	ArtifactKindModelRequest    ArtifactKind = "model_request"
	ArtifactKindModelResponse   ArtifactKind = "model_response"
	ArtifactKindToolArguments   ArtifactKind = "tool_arguments"
	ArtifactKindToolOutput      ArtifactKind = "tool_output"
)
