package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const defaultPromptCacheRetention = "24h"

func (r *Runner) promptCacheKey() string {
	if r == nil {
		return ""
	}
	conversationID := strings.TrimSpace(r.options.Metadata["conversation_id"])
	if conversationID == "" {
		return ""
	}
	providerID := strings.TrimSpace(r.options.Metadata["provider_id"])
	modelID := strings.TrimSpace(r.options.Metadata["model_id"])
	material := strings.Join([]string{"agent-runtime", providerID, modelID, conversationID}, "\n")
	sum := sha256.Sum256([]byte(material))
	return "agent-runtime-" + hex.EncodeToString(sum[:16])
}

func promptCacheRetentionForKey(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return defaultPromptCacheRetention
}
