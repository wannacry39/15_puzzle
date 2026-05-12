package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	openai "github.com/sashabaranov/go-openai"
)

const SystemPrompt = `Ты — игрок в игру «Пятнашки» (15 Puzzle).

Поле 4×4, числа от 1 до 15 и пустая клетка (_).
Победное состояние:
  1  2  3  4
  5  6  7  8
  9 10 11 12
 13 14 15  _

Сначала вызови get_state чтобы увидеть доску. В ответе будет поле grid (визуальная сетка) и last_moved (плитка которую двинули на прошлом ходу).
Затем вызови move чтобы сдвинуть одну плитку соседнюю с пустой клеткой.
Нельзя двигать плитку из last_moved — это отмена предыдущего хода.
Сделай ровно один успешный ход, продвигающий доску к решению.`

var ErrLLMUnavailable = errors.New("llm api unavailable")

type Player struct {
	client *openai.Client
	model  string
	tools  []openai.Tool
	log    zerolog.Logger
}

func NewPlayer(apiKey, model string, mcpTools []mcp.Tool, timeout time.Duration, log zerolog.Logger) *Player {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = "https://openrouter.ai/api/v1"
	cfg.HTTPClient = &http.Client{Timeout: timeout}
	return &Player{
		client: openai.NewClientWithConfig(cfg),
		model:  model,
		tools:  convertTools(mcpTools),
		log:    log,
	}
}

func (p *Player) Chat(ctx context.Context, messages []openai.ChatCompletionMessage, gameID string, step int) (openai.ChatCompletionMessage, error) {
	req := openai.ChatCompletionRequest{
		Model:       p.model,
		Messages:    messages,
		Tools:       p.tools,
		Temperature: 0.2,
	}

	backoff := 2 * time.Second
	for attempt := 1; attempt <= 5; attempt++ {
		start := time.Now()
		resp, err := p.client.CreateChatCompletion(ctx, req)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			var apiErr *openai.APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatusCode == 429 {
				p.log.Warn().
					Str("event", "llm_rate_limit").
					Str("gameId", gameID).
					Int("step", step).
					Int("attempt", attempt).
					Dur("backoff", backoff).
					Send()
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return openai.ChatCompletionMessage{}, ctx.Err()
				}
				backoff *= 2
				continue
			}
			p.log.Error().
				Str("event", "llm_error").
				Str("gameId", gameID).
				Int("step", step).
				Str("model", p.model).
				Int64("durationMs", dur).
				Err(err).
				Send()
			return openai.ChatCompletionMessage{}, fmt.Errorf("%w: %v", ErrLLMUnavailable, err)
		}
		if len(resp.Choices) == 0 {
			return openai.ChatCompletionMessage{}, fmt.Errorf("%w: empty choices", ErrLLMUnavailable)
		}
		msg := resp.Choices[0].Message
		p.log.Info().
			Str("event", "llm_call").
			Str("gameId", gameID).
			Int("step", step).
			Str("model", p.model).
			Str("finish_reason", string(resp.Choices[0].FinishReason)).
			Int("tool_calls", len(msg.ToolCalls)).
			Int64("durationMs", dur).
			Send()
		return msg, nil
	}
	return openai.ChatCompletionMessage{}, fmt.Errorf("%w: rate limit exceeded after retries", ErrLLMUnavailable)
}

func convertTools(mcpTools []mcp.Tool) []openai.Tool {
	tools := make([]openai.Tool, len(mcpTools))
	for i, t := range mcpTools {
		tools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return tools
}
