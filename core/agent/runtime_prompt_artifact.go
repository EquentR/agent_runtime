package agent

import (
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
)

const legacySystemPromptSegmentKey = "legacy_system_prompt"

type runnerRuntimePromptArtifact struct {
	Segments           []runtimeprompt.Segment `json:"segments,omitempty"`
	Messages           []model.Message         `json:"messages"`
	PromptMessageCount int                     `json:"prompt_message_count"`
	PhaseSegmentCounts map[string]int          `json:"phase_segment_counts,omitempty"`
	SourceCounts       map[string]int          `json:"source_counts,omitempty"`
}

func buildRunnerRuntimePromptArtifact(options Options, buildResult runtimeprompt.BuildResult, requestMessages []model.Message) runnerRuntimePromptArtifact {
	segments := filterRuntimePromptSegmentsForStep(buildResult.Envelope.Segments, buildResult.AfterToolTurn)
	artifact := runnerRuntimePromptArtifact{
		Segments:           cloneRuntimePromptSegments(segments),
		Messages:           cloneMessages(requestMessages),
		PromptMessageCount: promptMessageCountForRuntimePrompt(buildResult, requestMessages),
		PhaseSegmentCounts: countRuntimePromptSegmentsByPhase(segments),
		SourceCounts:       countRuntimePromptSegmentsBySource(segments),
	}

	if options.ResolvedPrompt == nil && options.SystemPrompt != "" {
		for i := range artifact.Segments {
			segment := &artifact.Segments[i]
			if strings.TrimSpace(segment.Phase) != runtimeprompt.PhaseSession {
				continue
			}
			if strings.TrimSpace(segment.SourceType) != runtimeprompt.SourceTypeResolvedPrompt {
				continue
			}
			if strings.TrimSpace(segment.SourceKey) != legacySystemPromptSegmentKey {
				continue
			}
			segment.Content = options.SystemPrompt
		}
	}

	return artifact
}

func buildRunnerRuntimePromptPayload(options Options, artifact runnerRuntimePromptArtifact) map[string]any {
	payload := map[string]any{
		"message_count":        len(artifact.Messages),
		"prompt_message_count": artifact.PromptMessageCount,
		"segment_count":        len(artifact.Segments),
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

func cloneRuntimePromptSegments(segments []runtimeprompt.Segment) []runtimeprompt.Segment {
	if len(segments) == 0 {
		return nil
	}
	cloned := make([]runtimeprompt.Segment, len(segments))
	copy(cloned, segments)
	return cloned
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
