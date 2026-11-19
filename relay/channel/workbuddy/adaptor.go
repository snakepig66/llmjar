package workbuddy

// WorkBuddy (copilot.tencent.com) is OpenAI-compatible at the wire level but
// only accepts streamed chat completions. This adaptor wraps the OpenAI one:
// every upstream request is forced to stream, and when the buyer asked for a
// non-streaming response the SSE stream is aggregated back into a single
// chat.completion JSON object.

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/marketplace"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ctxKeyBuyerStream records whether the buyer asked for a stream. The relay
// flips info.IsStream to true once it sees the upstream SSE content type, so
// the original intent must be captured before the request goes out.
const ctxKeyBuyerStream = "wb_buyer_stream"

type Adaptor struct {
	openai.Adaptor
}

func (a *Adaptor) GetChannelName() string {
	return "workbuddy"
}

// ConvertOpenAIRequest forces the upstream dialect WorkBuddy expects:
// stream=true, include_usage, and the classic max_tokens field (the desktop
// CLI maps max_completion_tokens onto max_tokens).
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	streamTrue := true
	request.Stream = &streamTrue
	if request.MaxCompletionTokens != nil && request.MaxTokens == nil {
		request.MaxTokens = request.MaxCompletionTokens
		request.MaxCompletionTokens = nil
	}
	if request.StreamOptions == nil {
		request.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	} else {
		request.StreamOptions.IncludeUsage = true
	}
	return a.Adaptor.ConvertOpenAIRequest(c, info, request)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if c != nil && info != nil {
		c.Set(ctxKeyBuyerStream, info.IsStream)
	}
	return a.Adaptor.DoRequest(c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	buyerStream, recorded := c.Get(ctxKeyBuyerStream)
	streamFlag, _ := buyerStream.(bool)
	if recorded && !streamFlag && info.RelayMode == relayconstant.RelayModeChatCompletions {
		return aggregateStream(c, info, resp)
	}
	return a.Adaptor.DoResponse(c, resp, info)
}

// streamChoice accumulates one choice's deltas across chunks.
type streamChoice struct {
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	role      string
	toolCalls map[int]*dto.ToolCallResponse
	toolOrder []int
}

func (sc *streamChoice) addDelta(d *dto.ChatCompletionsStreamResponseChoiceDelta) {
	sc.content.WriteString(d.GetContentString())
	sc.reasoning.WriteString(d.GetReasoningContent())
	if d.Role != "" {
		sc.role = d.Role
	}
	for _, tc := range d.ToolCalls {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		slot, ok := sc.toolCalls[idx]
		if !ok {
			slot = &dto.ToolCallResponse{Type: "function"}
			sc.toolCalls[idx] = slot
			sc.toolOrder = append(sc.toolOrder, idx)
		}
		if tc.ID != "" {
			slot.ID = tc.ID
		}
		slot.Function.Name += tc.Function.Name
		slot.Function.Arguments += tc.Function.Arguments
	}
}

// aggregateStream consumes a WorkBuddy SSE body and answers the buyer with
// one aggregated chat.completion object.
func aggregateStream(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(errors.New("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	choices := map[int]*streamChoice{}
	var order []int
	var last dto.ChatCompletionsStreamResponse
	var usage dto.Usage
	gotChunk := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" || payload == "" {
			continue
		}
		var chunk dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		gotChunk = true
		last = chunk
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		for i := range chunk.Choices {
			ch := &chunk.Choices[i]
			sc, ok := choices[ch.Index]
			if !ok {
				sc = &streamChoice{toolCalls: map[int]*dto.ToolCallResponse{}}
				choices[ch.Index] = sc
				order = append(order, ch.Index)
			}
			sc.addDelta(&ch.Delta)
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				sc.finish = *ch.FinishReason
			}
		}
	}
	if !gotChunk {
		return nil, types.NewOpenAIError(errors.New("workbuddy: upstream stream carried no data"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	full := dto.OpenAITextResponse{
		Id:      last.Id,
		Object:  "chat.completion",
		Created: last.Created,
		Model:   last.Model,
	}
	for _, idx := range order {
		sc := choices[idx]
		msg := dto.Message{Role: "assistant"}
		if sc.role != "" {
			msg.Role = sc.role
		}
		msg.SetStringContent(sc.content.String())
		if r := sc.reasoning.String(); r != "" {
			msg.ReasoningContent = &r
		}
		if len(sc.toolOrder) > 0 {
			toolCalls := make([]dto.ToolCallResponse, 0, len(sc.toolOrder))
			for _, ti := range sc.toolOrder {
				toolCalls = append(toolCalls, *sc.toolCalls[ti])
			}
			msg.SetToolCalls(toolCalls)
		}
		full.Choices = append(full.Choices, dto.OpenAITextResponseChoice{
			Index:        idx,
			Message:      msg,
			FinishReason: sc.finish,
		})
	}
	full.Usage = usage

	// Fallback usage when the upstream stream omits a usage frame.
	if full.Usage.PromptTokens == 0 && full.Usage.CompletionTokens == 0 {
		completion := 0
		for _, ch := range full.Choices {
			completion += len(ch.Message.StringContent()) + len(ch.Message.GetReasoningContent())
		}
		completion /= 4
		full.Usage = dto.Usage{
			PromptTokens:     info.GetEstimatePromptTokens(),
			CompletionTokens: completion,
			TotalTokens:      info.GetEstimatePromptTokens() + completion,
		}
	}

	info.ObserveResponseModel(full.Model)
	if !marketplace.HasModelRewrite(c) && info.OriginModelName != "" {
		full.Model = info.OriginModelName
	}
	if err := helper.ObjectData(c, full); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return &full.Usage, nil
}

var _ channel.Adaptor = (*Adaptor)(nil)
