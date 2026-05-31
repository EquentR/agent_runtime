package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type LoopGuardDecision string

const (
	LoopGuardAllow LoopGuardDecision = "allow"
	LoopGuardWarn  LoopGuardDecision = "warn"
	LoopGuardStop  LoopGuardDecision = "stop"
)

const (
	LoopGuardReasonSameTargetWriteRepeated = "same_target_write_repeated"
	LoopGuardReasonSameTargetReadRepeated  = "same_target_read_repeated"
	LoopGuardReasonSameSearchRepeated      = "same_search_repeated"
	LoopGuardReasonSameErrorRepeated       = "same_error_repeated"

	LoopGuardStopStrategySafeCompletion = "safe_completion"
	LoopGuardStopStrategyFailedLoop     = "failed_loop"
)

type LoopGuardOptions struct {
	Enabled                    *bool
	SameTargetSuccessSoftLimit int
	SameTargetSuccessHardLimit int
	SameErrorSoftLimit         int
	SameErrorHardLimit         int
}

type LoopGuardResult struct {
	Decision     LoopGuardDecision
	Reason       string
	ToolName     string
	Operation    string
	Target       string
	RepeatCount  int
	SoftLimit    int
	HardLimit    int
	StopStrategy string
	WarningText  string
	FinalMessage string
	Err          error
}

type LoopGuard struct {
	options LoopGuardOptions
	lastKey string
	lastRun int
}

type loopGuardProfile struct {
	ToolName            string
	OperationKind       string
	TargetKey           string
	TargetDisplay       string
	ArgumentFingerprint string
	OutcomeKind         string
	ErrorFingerprint    string
	NormalizedError     string
	Reason              string
	StopStrategy        string
	Countable           bool
}

func NewLoopGuard(options LoopGuardOptions) *LoopGuard {
	return &LoopGuard{options: normalizeLoopGuardOptions(options)}
}

func (g *LoopGuard) AfterToolResult(call coretypes.ToolCall, arguments map[string]interface{}, output string, err error) LoopGuardResult {
	_ = output

	result := LoopGuardResult{Decision: LoopGuardAllow}
	if g == nil {
		return result
	}
	if call.Name == "ask_user" {
		g.reset()
		return result
	}
	if !loopGuardOptionsEnabled(g.options) {
		return result
	}
	if arguments == nil {
		decoded, decodeErr := decodeToolArguments(call)
		if decodeErr == nil {
			arguments = decoded
		} else {
			arguments = map[string]interface{}{}
		}
	}

	profile := profileLoopGuardToolResult(call, arguments, err)
	if !profile.Countable {
		g.reset()
		return result
	}

	key := profile.key()
	if key == g.lastKey {
		g.lastRun++
	} else {
		g.lastKey = key
		g.lastRun = 1
	}

	softLimit, hardLimit := g.limitsFor(profile)
	result = LoopGuardResult{
		Decision:     LoopGuardAllow,
		Reason:       profile.Reason,
		ToolName:     profile.ToolName,
		Operation:    profile.OperationKind,
		Target:       profile.TargetDisplay,
		RepeatCount:  g.lastRun,
		SoftLimit:    softLimit,
		HardLimit:    hardLimit,
		StopStrategy: "",
	}
	if g.lastRun >= hardLimit {
		result.Decision = LoopGuardStop
		result.StopStrategy = profile.StopStrategy
		switch profile.StopStrategy {
		case LoopGuardStopStrategySafeCompletion:
			result.FinalMessage = fmt.Sprintf("文件已写入 `%s`。运行时检测到模型正在重复覆盖同一文件，因此已停止继续调用工具。", profile.TargetDisplay)
		case LoopGuardStopStrategyFailedLoop:
			result.Err = loopGuardStopError(profile)
		}
		return result
	}
	if g.lastRun >= softLimit {
		result.Decision = LoopGuardWarn
		result.WarningText = loopGuardWarningText(profile)
	}
	return result
}

func normalizeLoopGuardOptions(options LoopGuardOptions) LoopGuardOptions {
	if options.Enabled == nil {
		enabled := true
		options.Enabled = &enabled
	}
	if !*options.Enabled {
		return options
	}
	if options.SameTargetSuccessSoftLimit <= 0 {
		options.SameTargetSuccessSoftLimit = 2
	}
	if options.SameTargetSuccessHardLimit <= 0 {
		options.SameTargetSuccessHardLimit = 3
	}
	if options.SameErrorSoftLimit <= 0 {
		options.SameErrorSoftLimit = 2
	}
	if options.SameErrorHardLimit <= 0 {
		options.SameErrorHardLimit = 3
	}
	if options.SameTargetSuccessHardLimit < options.SameTargetSuccessSoftLimit {
		options.SameTargetSuccessHardLimit = options.SameTargetSuccessSoftLimit
	}
	if options.SameErrorHardLimit < options.SameErrorSoftLimit {
		options.SameErrorHardLimit = options.SameErrorSoftLimit
	}
	return options
}

func loopGuardOptionsEnabled(options LoopGuardOptions) bool {
	return options.Enabled == nil || *options.Enabled
}

func (g *LoopGuard) reset() {
	g.lastKey = ""
	g.lastRun = 0
}

func (g *LoopGuard) limitsFor(profile loopGuardProfile) (int, int) {
	if profile.OutcomeKind == "error" {
		return g.options.SameErrorSoftLimit, g.options.SameErrorHardLimit
	}
	return g.options.SameTargetSuccessSoftLimit, g.options.SameTargetSuccessHardLimit
}

func profileLoopGuardToolResult(call coretypes.ToolCall, arguments map[string]interface{}, err error) loopGuardProfile {
	if err != nil {
		return profileLoopGuardError(call, arguments, err)
	}
	return profileLoopGuardSuccess(call, arguments)
}

func profileLoopGuardSuccess(call coretypes.ToolCall, arguments map[string]interface{}) loopGuardProfile {
	switch call.Name {
	case "write_file":
		path := argumentString(arguments, "path")
		mode := writeMode(arguments)
		targetKey := path + "\x00" + mode
		targetDisplay := path
		if mode != "overwrite" {
			lineRange := writeLineRangeFingerprint(arguments)
			targetKey += "\x00" + lineRange
			if lineRange != "" {
				targetDisplay = path + ":" + lineRange
			}
		}
		return loopGuardProfile{
			ToolName:      call.Name,
			OperationKind: mode,
			TargetKey:     targetKey,
			TargetDisplay: targetDisplay,
			OutcomeKind:   "success",
			Reason:        LoopGuardReasonSameTargetWriteRepeated,
			StopStrategy:  writeLoopGuardStopStrategy(mode),
			Countable:     path != "",
		}
	case "read_file":
		path := argumentString(arguments, "path")
		startLine := argumentString(arguments, "start_line")
		lineCount := argumentString(arguments, "line_count")
		target := strings.Join([]string{path, startLine, lineCount}, ":")
		return loopGuardProfile{
			ToolName:      call.Name,
			OperationKind: "read_window",
			TargetKey:     target,
			TargetDisplay: target,
			OutcomeKind:   "success",
			Reason:        LoopGuardReasonSameTargetReadRepeated,
			StopStrategy:  LoopGuardStopStrategyFailedLoop,
			Countable:     path != "",
		}
	case "search_file", "grep_file":
		fingerprint := fingerprintArguments(arguments)
		return loopGuardProfile{
			ToolName:            call.Name,
			OperationKind:       "search",
			TargetKey:           fingerprint,
			TargetDisplay:       searchTargetDisplay(arguments, fingerprint),
			ArgumentFingerprint: fingerprint,
			OutcomeKind:         "success",
			Reason:              LoopGuardReasonSameSearchRepeated,
			StopStrategy:        LoopGuardStopStrategyFailedLoop,
			Countable:           true,
		}
	default:
		return loopGuardProfile{ToolName: call.Name}
	}
}

func profileLoopGuardError(call coretypes.ToolCall, arguments map[string]interface{}, err error) loopGuardProfile {
	fingerprint := fingerprintArguments(arguments)
	normalizedError := normalizeLoopGuardError(err)
	operation := call.Name
	target := ""

	switch call.Name {
	case "list_files":
		operation = "list"
		target = argumentString(arguments, "path")
	case "write_file":
		operation = writeMode(arguments)
		target = argumentString(arguments, "path")
	case "read_file":
		operation = "read_window"
		target = argumentString(arguments, "path")
	case "search_file", "grep_file":
		operation = "search"
		target = searchTargetDisplay(arguments, fingerprint)
	}
	if operation == "" {
		operation = call.Name
	}

	return loopGuardProfile{
		ToolName:            call.Name,
		OperationKind:       operation,
		TargetKey:           target,
		TargetDisplay:       target,
		ArgumentFingerprint: fingerprint,
		OutcomeKind:         "error",
		ErrorFingerprint:    normalizedError,
		NormalizedError:     normalizedError,
		Reason:              LoopGuardReasonSameErrorRepeated,
		StopStrategy:        LoopGuardStopStrategyFailedLoop,
		Countable:           normalizedError != "",
	}
}

func writeLoopGuardStopStrategy(mode string) string {
	if mode == "overwrite" {
		return LoopGuardStopStrategySafeCompletion
	}
	return LoopGuardStopStrategyFailedLoop
}

func loopGuardStopError(profile loopGuardProfile) error {
	if profile.OutcomeKind == "error" {
		return fmt.Errorf("%w: %s repeatedly failed with %q", ErrToolLoopDetected, profile.ToolName, profile.NormalizedError)
	}
	return fmt.Errorf("%w: %s repeatedly succeeded for %q", ErrToolLoopDetected, profile.ToolName, profile.TargetDisplay)
}

func writeMode(arguments map[string]interface{}) string {
	mode := strings.ToLower(argumentString(arguments, "mode"))
	if mode == "" {
		return "overwrite"
	}
	return mode
}

func (p loopGuardProfile) key() string {
	parts := []string{
		p.ToolName,
		p.OperationKind,
		p.TargetKey,
		p.OutcomeKind,
	}
	if p.ArgumentFingerprint != "" {
		parts = append(parts, p.ArgumentFingerprint)
	}
	if p.ErrorFingerprint != "" {
		parts = append(parts, p.ErrorFingerprint)
	}
	return strings.Join(parts, "\x00")
}

func loopGuardWarningText(profile loopGuardProfile) string {
	if profile.OutcomeKind == "error" {
		return fmt.Sprintf("The previous %s call failed with the same error for the same arguments: %q. Do not call %s again unless the inputs have changed.", profile.ToolName, profile.NormalizedError, profile.ToolName)
	}
	if profile.ToolName != "write_file" {
		return fmt.Sprintf("The previous %s call already succeeded for this target `%s`. Do not call %s again with the same arguments unless the user explicitly requested it. If the task is complete, respond without tool calls.", profile.ToolName, profile.TargetDisplay, profile.ToolName)
	}
	return fmt.Sprintf("The previous %s call already succeeded for this target `%s`. Do not call %s again unless the user explicitly requested another revision. If the task is complete, respond without tool calls and mention only the saved path.", profile.ToolName, profile.TargetDisplay, profile.ToolName)
}

func argumentString(arguments map[string]interface{}, key string) string {
	value, ok := arguments[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func writeLineRangeFingerprint(arguments map[string]interface{}) string {
	keys := []string{"start_line", "end_line", "line_start", "line_end", "line", "line_count", "range"}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := argumentString(arguments, key)
		if value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, ",")
}

func searchTargetDisplay(arguments map[string]interface{}, fallback string) string {
	path := argumentString(arguments, "path")
	query := argumentString(arguments, "query")
	pattern := argumentString(arguments, "pattern")
	switch {
	case path != "" && query != "":
		return path + ":" + query
	case path != "" && pattern != "":
		return path + ":" + pattern
	case query != "":
		return query
	case pattern != "":
		return pattern
	case path != "":
		return path
	default:
		return fallback
	}
}

func fingerprintArguments(arguments map[string]interface{}) string {
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		raw = []byte(fmt.Sprint(arguments))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeLoopGuardError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(err.Error())), " ")
}
