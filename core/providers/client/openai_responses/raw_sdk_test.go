package openai_official

import (
	"context"
	"os"
	"testing"
	"time"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/tools/builtin"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

func TestRawSdkFC(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	modelID := os.Getenv("OPENAI_MODEL")
	if apiKey == "" || baseURL == "" || modelID == "" {
		t.Skip("set OPENAI_API_KEY, OPENAI_BASE_URL, and OPENAI_MODEL to run live test")
	}

	client := NewOpenAiResponsesClient(apiKey, baseURL, 60*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	registry := coretools.NewRegistry()
	wd, _ := os.Getwd()
	err := builtin.Register(registry, builtin.Options{WorkspaceRoot: wd})
	if err != nil {
		t.Fatal(err)
	}

	input := make(responses.ResponseInputParam, 0)
	input = append(input, responses.ResponseInputItemParamOfMessage("获取README.md的内容，然后返回200字摘要给我", toResponseRole("user")))

	resp, err := client.cli.Responses.New(ctx, responses.ResponseNewParams{
		Model: modelID,
		Tools: modelToolsToResponse(registry.List()),
		Reasoning: shared.ReasoningParam{
			Summary: shared.ReasoningSummaryAuto,
			Effort:  shared.ReasoningEffortMedium,
		},
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Log(resp.RawJSON())

	// 响应id重放
	//resp2, err := client.cli.Responses.New(ctx, responses.ResponseNewParams{
	//	Model: modelID,
	//	Tools: modelToolsToResponse(registry.List()),
	//	PreviousResponseID: openai.String(resp.PreviousResponseID),
	//})
	//

}
