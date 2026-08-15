package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
)

type runnerPromptSegmentSummary struct {
	Phase      string `json:"phase,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	SourceKey  string `json:"source_key,omitempty"`
	SizeBytes  int    `json:"size_bytes,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
}

type runnerRuntimePromptArtifact struct {
	Segments           []runnerPromptSegmentSummary `json:"segments,omitempty"`
	PromptMessageCount int                          `json:"prompt_message_count"`
	SegmentCount       int                          `json:"segment_count"`
	PhaseSegmentCounts map[string]int               `json:"phase_segment_counts,omitempty"`
	SourceCounts       map[string]int               `json:"source_counts,omitempty"`
}

func buildRunnerRuntimePromptArtifact(options Options, buildResult runtimeprompt.BuildResult, requestMessages []model.Message) runnerRuntimePromptArtifact {
	segments := filterRuntimePromptSegmentsForStep(buildResult.Envelope.Segments, buildResult.AfterToolTurn)
	return runnerRuntimePromptArtifact{
		Segments:           summarizeRuntimePromptSegments(segments),
		PromptMessageCount: promptMessageCountForRuntimePrompt(buildResult, requestMessages),
		SegmentCount:       len(segments),
		PhaseSegmentCounts: countRuntimePromptSegmentsByPhase(segments),
		SourceCounts:       countRuntimePromptSegmentsBySource(segments),
	}
}

func buildRunnerRuntimePromptPayload(options Options, artifact runnerRuntimePromptArtifact) map[string]any {
	payload := map[string]any{
		"prompt_message_count": artifact.PromptMessageCount,
		"segment_count":        artifact.SegmentCount,
	}
	if options.ResolvedPrompt != nil {
		if scene := strings.TrimSpace(options.ResolvedPrompt.Scene); scene != "" {
			payload["scene"] = scene
		}
	}
	if len(artifact.PhaseSegmentCounts) > 0 {
		payload["phase_segment_counts"] = cloneIntMap(artifact.PhaseSegmentCounts)
	}
	if len(artifact.SourceCounts) > 0 {
		payload["source_counts"] = cloneIntMap(artifact.SourceCounts)
	}
	return payload
}

func summarizeRuntimePromptSegments(segments []runtimeprompt.Segment) []runnerPromptSegmentSummary {
	if len(segments) == 0 {
		return nil
	}
	summaries := make([]runnerPromptSegmentSummary, 0, len(segments))
	for _, segment := range segments {
		summaries = append(summaries, runnerPromptSegmentSummary{
			Phase:      strings.TrimSpace(segment.Phase),
			SourceType: strings.TrimSpace(segment.SourceType),
			SourceKey:  strings.TrimSpace(segment.SourceKey),
			SizeBytes:  len(segment.Content),
			SHA256:     sha256Hex(segment.Content),
		})
	}
	return summaries
}

func filterRuntimePromptSegmentsForStep(segments []runtimeprompt.Segment, afterToolTurn bool) []runtimeprompt.Segment {
	if len(segments) == 0 {
		return nil
	}
	filtered := make([]runtimeprompt.Segment, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.Phase) == runtimeprompt.PhaseToolResult && !afterToolTurn {
			continue
		}
		filtered = append(filtered, segment)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func promptMessageCountForRuntimePrompt(buildResult runtimeprompt.BuildResult, requestMessages []model.Message) int {
	promptMessageCount := len(requestMessages) - len(buildResult.Body)
	if promptMessageCount < 0 {
		return 0
	}
	return promptMessageCount
}

func countRuntimePromptSegmentsByPhase(segments []runtimeprompt.Segment) map[string]int {
	if len(segments) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, segment := range segments {
		phase := strings.TrimSpace(segment.Phase)
		if phase == "" {
			continue
		}
		counts[phase]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func countRuntimePromptSegmentsBySource(segments []runtimeprompt.Segment) map[string]int {
	if len(segments) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, segment := range segments {
		sourceType := strings.TrimSpace(segment.SourceType)
		if sourceType == "" {
			continue
		}
		counts[sourceType]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
