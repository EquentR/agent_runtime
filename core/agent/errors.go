package agent

import "errors"

var (
	ErrNilClient        = errors.New("agent client is required")
	ErrMaxStepsExceeded = errors.New("agent max steps exceeded")
	ErrToolLoopDetected = errors.New("tool loop detected")
)
