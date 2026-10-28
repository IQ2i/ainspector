package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/shared/constant"

	"github.com/iq2i/ainspector/internal/llm"
)

// defaultMaxTokens leaves room for adaptive thinking: the limit applies to thinking and answer together
const defaultMaxTokens = 16000

// requestTimeout bounds each attempt of a request: the SDK default of ten minutes would let one
// stuck call hold a job. A variable so that tests can shorten it.
var requestTimeout = 5 * time.Minute

// Client calls the Anthropic Messages API.
type Client struct {
	client sdk.Client
	model  string
}

// New creates a client. An empty model leaves the choice to each request.
func New(apiKey, model string, opts ...option.RequestOption) *Client {
	defaults := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(requestTimeout),
		option.WithMaxRetries(2),
	}
	opts = append(defaults, opts...)
	return &Client{client: sdk.NewClient(opts...), model: model}
}

// Complete sends a request. A refusal that no fallback model could serve is an error;
// with a Schema, the reply is valid JSON or an error.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	if model == "" {
		return llm.Response{}, fmt.Errorf("no model configured")
	}

	msg, err := c.client.Beta.Messages.New(ctx, buildParams(model, req))
	if err != nil {
		return llm.Response{}, fmt.Errorf("anthropic request failed: %w", err)
	}

	resp := toResponse(msg)

	// From here on the tokens were billed: every error carries the response so that the
	// caller can account for them
	if msg.StopReason == sdk.BetaStopReasonRefusal {
		category := string(msg.StopDetails.Category)
		if category == "" {
			category = "unspecified"
		}
		return resp, fmt.Errorf("request refused by the model (%s): %s", category, msg.StopDetails.Explanation)
	}

	if req.Schema != nil {
		if resp.Truncated {
			return resp, fmt.Errorf("response truncated at %d output tokens before the JSON document was complete", resp.Usage.Output)
		}
		if !json.Valid([]byte(resp.Text)) {
			return resp, fmt.Errorf("response is not a valid JSON document")
		}
		resp.JSON = json.RawMessage(resp.Text)
	}
	return resp, nil
}

// toResponse converts a message. After a server-side fallback the content holds the output of
// the model that declined, a fallback block, then the answer of the fallback model: only the
// latter is the answer. Usage then sums every attempt, as the top-level usage only covers the last.
func toResponse(msg *sdk.BetaMessage) llm.Response {
	var text strings.Builder
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case sdk.BetaFallbackBlock:
			text.Reset()
		case sdk.BetaTextBlock:
			text.WriteString(b.Text)
		}
	}

	resp := llm.Response{
		Text:      text.String(),
		Model:     string(msg.Model),
		Truncated: msg.StopReason == sdk.BetaStopReasonMaxTokens,
		Usage: llm.Usage{
			Input:      int(msg.Usage.InputTokens),
			Output:     int(msg.Usage.OutputTokens),
			CacheRead:  int(msg.Usage.CacheReadInputTokens),
			CacheWrite: int(msg.Usage.CacheCreationInputTokens),
		},
	}

	var attempts []llm.Attempt
	served := false
	for _, it := range msg.Usage.Iterations {
		switch it.Type {
		case "fallback_message":
			served = true
		case "message":
		default:
			continue
		}
		attempts = append(attempts, llm.Attempt{Model: string(it.Model), Usage: llm.Usage{
			Input:      int(it.InputTokens),
			Output:     int(it.OutputTokens),
			CacheRead:  int(it.CacheReadInputTokens),
			CacheWrite: int(it.CacheCreationInputTokens),
		}})
	}
	if served {
		resp.Attempts = attempts
		resp.Usage = llm.Usage{}
		for _, a := range attempts {
			resp.Usage.Input += a.Usage.Input
			resp.Usage.Output += a.Usage.Output
			resp.Usage.CacheRead += a.Usage.CacheRead
			resp.Usage.CacheWrite += a.Usage.CacheWrite
		}
	}
	return resp
}

func buildParams(model string, req llm.Request) sdk.BetaMessageNewParams {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	params := sdk.BetaMessageNewParams{
		Model:     sdk.Model(model),
		MaxTokens: int64(maxTokens),
		Thinking:  sdk.BetaThinkingConfigParamUnion{OfAdaptive: &sdk.BetaThinkingConfigAdaptiveParam{}},
		OutputConfig: sdk.BetaOutputConfigParam{
			Effort: sdk.BetaOutputConfigEffortMedium,
		},
		// A refusal is served again by another model within the same call
		Fallbacks: sdk.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		Betas:     []sdk.AnthropicBeta{sdk.AnthropicBetaServerSideFallback2026_07_01},
	}

	for _, b := range req.System {
		block := sdk.BetaTextBlockParam{Text: b.Text}
		if b.Cache {
			block.CacheControl = sdk.NewBetaCacheControlEphemeralParam()
		}
		params.System = append(params.System, block)
	}

	for _, m := range req.Messages {
		blocks := []sdk.BetaContentBlockParamUnion{sdk.NewBetaTextBlock(m.Content)}
		if m.Role == "assistant" {
			params.Messages = append(params.Messages, sdk.BetaMessageParam{OfAssistant: &sdk.BetaAssistantMessageParam{Content: blocks}})
		} else {
			params.Messages = append(params.Messages, sdk.NewBetaUserMessage(blocks...))
		}
	}

	if req.Schema != nil {
		params.OutputConfig.Format = sdk.BetaJSONOutputFormatParam{Schema: req.Schema.JSON}
	}
	return params
}
