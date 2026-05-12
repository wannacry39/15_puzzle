package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	openai "github.com/sashabaranov/go-openai"
)

const SystemPrompt = `Ты — проверяющий в игре «Пятнашки» (15 Puzzle).

Победное состояние: [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,0]

Вызови get_state чтобы получить текущее состояние доски.
Затем ответь ТОЛЬКО одним словом: true (если игра решена) или false (если нет).`

var ErrLLMUnavailable = errors.New("llm api unavailable")

type Checker struct {
	client *openai.Client
	model  string
	tools  []openai.Tool
	log    zerolog.Logger
}

func NewChecker(apiKey, model string, mcpTools []mcp.Tool, timeout time.Duration, log zerolog.Logger) *Checker {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = "https://openrouter.ai/api/v1"
	cfg.HTTPClient = &http.Client{Timeout: timeout}
	return &Checker{
		client: openai.NewClientWithConfig(cfg),
		model:  model,
		tools:  convertTools(mcpTools),
		log:    log,
	}
}

func (c *Checker) Chat(ctx context.Context, messages []openai.ChatCompletionMessage, gameID string, step int) (openai.ChatCompletionMessage, error) {
	req := openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       c.tools,
		Temperature: 0.0,
	}

	backoff := 2 * time.Second
	for attempt := 1; attempt <= 5; attempt++ {
		start := time.Now()
		resp, err := c.client.CreateChatCompletion(ctx, req)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			var apiErr *openai.APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatusCode == 429 {
				c.log.Warn().
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
			c.log.Error().
				Str("event", "llm_error").
				Str("gameId", gameID).
				Int("step", step).
				Str("model", c.model).
				Int64("durationMs", dur).
				Err(err).
				Send()
			return openai.ChatCompletionMessage{}, fmt.Errorf("%w: %v", ErrLLMUnavailable, err)
		}
		if len(resp.Choices) == 0 {
			return openai.ChatCompletionMessage{}, fmt.Errorf("%w: empty choices", ErrLLMUnavailable)
		}
		msg := resp.Choices[0].Message
		c.log.Info().
			Str("event", "llm_call").
			Str("gameId", gameID).
			Int("step", step).
			Str("model", c.model).
			Str("finish_reason", string(resp.Choices[0].FinishReason)).
			Int("tool_calls", len(msg.ToolCalls)).
			Int64("durationMs", dur).
			Send()
		return msg, nil
	}
	return openai.ChatCompletionMessage{}, fmt.Errorf("%w: rate limit exceeded after retries", ErrLLMUnavailable)
}

func ParseBool(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(t, "true")
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
