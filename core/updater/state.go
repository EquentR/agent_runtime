package updater

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrGenerationConflict = errors.New("updater state generation conflict")

type OperationPhase string

const (
	PhaseIdle             OperationPhase = "idle"
	PhaseChecking         OperationPhase = "checking"
	PhaseAvailable        OperationPhase = "available"
	PhaseDownloading      OperationPhase = "downloading"
	PhaseVerifying        OperationPhase = "verifying"
	PhaseStaged           OperationPhase = "staged"
	PhaseDraining         OperationPhase = "draining"
	PhaseBackingUp        OperationPhase = "backing_up"
	PhaseReplacing        OperationPhase = "replacing"
	PhaseRestarting       OperationPhase = "restarting"
	PhaseHealthCheck      OperationPhase = "health_check"
	PhaseSucceeded        OperationPhase = "succeeded"
	PhaseFailed           OperationPhase = "failed"
	PhaseRollingBack      OperationPhase = "rolling_back"
	PhaseRolledBack       OperationPhase = "rolled_back"
	PhaseRecoveryRequired OperationPhase = "recovery_required"
)

var allowedTransitions = map[OperationPhase]map[OperationPhase]struct{}{
	PhaseIdle:        {PhaseChecking: {}, PhaseRollingBack: {}},
	PhaseChecking:    {PhaseAvailable: {}, PhaseIdle: {}, PhaseFailed: {}},
	PhaseAvailable:   {PhaseDownloading: {}, PhaseChecking: {}, PhaseIdle: {}, PhaseRollingBack: {}},
	PhaseDownloading: {PhaseVerifying: {}, PhaseFailed: {}},
	PhaseVerifying:   {PhaseStaged: {}, PhaseFailed: {}},
	PhaseStaged:      {PhaseDraining: {}, PhaseFailed: {}, PhaseIdle: {}},
	PhaseDraining:    {PhaseBackingUp: {}, PhaseFailed: {}, PhaseIdle: {}},
	PhaseBackingUp:   {PhaseReplacing: {}, PhaseFailed: {}},
	PhaseReplacing:   {PhaseRestarting: {}, PhaseRollingBack: {}, PhaseRecoveryRequired: {}},
	PhaseRestarting:  {PhaseHealthCheck: {}, PhaseRollingBack: {}, PhaseRecoveryRequired: {}},
	PhaseHealthCheck: {PhaseSucceeded: {}, PhaseRollingBack: {}, PhaseRecoveryRequired: {}},
	PhaseRollingBack: {PhaseRolledBack: {}, PhaseRecoveryRequired: {}, PhaseSucceeded: {}, PhaseAvailable: {}, PhaseIdle: {}},
	PhaseSucceeded:   {PhaseIdle: {}, PhaseChecking: {}, PhaseRollingBack: {}},
	PhaseFailed:      {PhaseIdle: {}, PhaseChecking: {}, PhaseDownloading: {}},
	PhaseRolledBack:  {PhaseIdle: {}, PhaseChecking: {}},
}

func CanTransition(from, to OperationPhase) bool {
	_, ok := allowedTransitions[from][to]
	return ok
}

func IsActiveOperationPhase(phase OperationPhase) bool {
	switch phase {
	case PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining, PhaseBackingUp, PhaseReplacing, PhaseRestarting, PhaseHealthCheck, PhaseRollingBack:
		return true
	default:
		return false
	}
}

type OperationState struct {
	OperationID     string         `json:"operation_id,omitempty"`
	Action          string         `json:"action,omitempty"`
	RequestTarget   string         `json:"request_target,omitempty"`
	Phase           OperationPhase `json:"phase"`
	Generation      int64          `json:"generation"`
	CurrentVersion  string         `json:"current_version,omitempty"`
	TargetVersion   string         `json:"target_version,omitempty"`
	BackupID        string         `json:"backup_id,omitempty"`
	BackupMode      BackupMode     `json:"backup_mode,omitempty"`
	BackupCreatedAt time.Time      `json:"backup_created_at,omitempty"`
	BackupVersion   string         `json:"backup_version,omitempty"`
	Error           string         `json:"error,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type OperationEvent struct {
	OperationID string         `json:"operation_id"`
	Phase       OperationPhase `json:"phase"`
	At          time.Time      `json:"at"`
	Message     string         `json:"message,omitempty"`
}

type StateStore struct {
	root       string
	statePath  string
	eventsPath string
	lockPath   string
	writeState func(string, any, os.FileMode) error
	owner      *FileOwner
	mu         sync.Mutex
}

func (s *StateStore) SetOwner(owner *FileOwner) {
	if s != nil {
		s.owner = owner
	}
}

func NewStateStore(root string) (*StateStore, error) {
	root, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &StateStore{root: root, statePath: filepath.Join(root, "state.json"), eventsPath: filepath.Join(root, "update.log"), lockPath: filepath.Join(root, "operation.lock"), writeState: writeJSONAtomic}, nil
}

func (s *StateStore) StatePath() string {
	if s == nil {
		return ""
	}
	return s.statePath
}

func (s *StateStore) Load() (OperationState, error) {
	if s == nil {
		return OperationState{}, fmt.Errorf("state store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := AcquireProcessLock(s.lockPath)
	if err != nil {
		return OperationState{}, err
	}
	defer lock.Close()
	return s.loadUnlocked()
}

func (s *StateStore) loadLocked(lock *ProcessLock) (OperationState, error) {
	if s == nil || lock == nil {
		return OperationState{}, fmt.Errorf("state store and process lock are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked()
}

func (s *StateStore) Save(expectedGeneration int64, state OperationState) (OperationState, error) {
	if s == nil {
		return OperationState{}, fmt.Errorf("state store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := AcquireProcessLock(s.lockPath)
	if err != nil {
		return OperationState{}, err
	}
	defer lock.Close()
	return s.saveLocked(lock, expectedGeneration, state)
}

func (s *StateStore) saveLockedWithLock(lock *ProcessLock, expectedGeneration int64, state OperationState) (OperationState, error) {
	if s == nil || lock == nil {
		return OperationState{}, fmt.Errorf("state store and process lock are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(lock, expectedGeneration, state)
}

func (s *StateStore) saveLocked(_ *ProcessLock, expectedGeneration int64, state OperationState) (OperationState, error) {
	current, err := s.loadUnlocked()
	if err != nil {
		return OperationState{}, err
	}
	if current.Generation != expectedGeneration {
		return OperationState{}, ErrGenerationConflict
	}
	if state.Phase == "" {
		state.Phase = PhaseIdle
	}
	if current.Generation > 0 && current.Phase != state.Phase && !CanTransition(current.Phase, state.Phase) {
		return OperationState{}, fmt.Errorf("invalid updater transition %q -> %q", current.Phase, state.Phase)
	}
	state.Generation = current.Generation + 1
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	} else {
		state.UpdatedAt = state.UpdatedAt.UTC()
	}
	writer := s.writeState
	if writer == nil {
		writer = writeJSONAtomic
	}
	if err := writer(s.statePath, state, 0o600); err != nil {
		return OperationState{}, err
	}
	if err := s.owner.apply(s.statePath); err != nil {
		return OperationState{}, err
	}
	if err := s.appendEventUnlocked(OperationEvent{OperationID: state.OperationID, Phase: state.Phase, At: state.UpdatedAt, Message: state.Error}); err != nil {
		return OperationState{}, err
	}
	return state, nil
}

func (s *StateStore) AppendEvent(event OperationEvent) error {
	if s == nil {
		return fmt.Errorf("state store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := AcquireProcessLock(s.lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	return s.appendEventUnlocked(event)
}

func (s *StateStore) appendEventUnlocked(event OperationEvent) error {
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	} else {
		event.At = event.At.UTC()
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(s.eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return s.owner.apply(s.eventsPath)
}

func (s *StateStore) Events() ([]OperationEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("state store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := AcquireProcessLock(s.lockPath)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	file, err := os.Open(s.eventsPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var events []OperationEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		var event OperationEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func (s *StateStore) loadUnlocked() (OperationState, error) {
	raw, err := os.ReadFile(s.statePath)
	if os.IsNotExist(err) {
		return OperationState{Phase: PhaseIdle}, nil
	}
	if err != nil {
		return OperationState{}, err
	}
	var state OperationState
	if err := json.Unmarshal(raw, &state); err != nil {
		return OperationState{}, fmt.Errorf("decode updater state: %w", err)
	}
	if state.Phase == "" {
		state.Phase = PhaseIdle
	}
	return state, nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func canonicalRoot(root string) (string, error) {
	root = filepath.Clean(root)
	if root == "" || root == "." {
		return "", fmt.Errorf("root path is required")
	}
	if err := ensureNoSymlink(root); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return abs, nil
}
