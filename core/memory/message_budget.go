package memory

import model "github.com/EquentR/agent_runtime/core/providers/types"

func BudgetPayloadForMessage(message model.Message) string {
	return budgetPayloadForMessage(message)
}

func SummarizeMessageForBudget(message model.Message) string {
	return summarizeMessage(message)
}

func CountMessageTokens(counter TokenCounter, messages []model.Message) int64 {
	if counter == nil || len(messages) == 0 {
		return 0
	}
	payloads := make([]string, 0, len(messages))
	for _, message := range messages {
		payload := BudgetPayloadForMessage(message)
		if payload == "" {
			continue
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		return 0
	}
	return int64(counter.CountMessages(payloads))
}

func CountRuntimeContextTokens(counter TokenCounter, state RuntimeContext) int64 {
	messages := make([]model.Message, 0, len(state.Tail)+1)
	if state.Recap != nil {
		messages = append(messages, cloneMessage(*state.Recap))
	}
	messages = append(messages, cloneMessages(state.Tail)...)
	return CountMessageTokens(counter, messages)
}
