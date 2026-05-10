package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mistral "github.com/gage-technologies/mistral-go"
	"github.com/rs/zerolog"
)

const systemPrompt = `Ты — проверяющий в игре «Пятнашки» (15 Puzzle).

Победное состояние поля: [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,0]

Тебе передаётся текущее состояние поля в виде плоского массива из 16 чисел.

Твоя задача: проверить, совпадает ли переданный массив с победным состоянием.

Отвечай ТОЛЬКО одним словом: true или false.
Никаких пояснений, только true или false.`

var ErrMistralUnavailable = errors.New("mistral api unavailable")

type Checker struct {
	mistral *mistral.MistralClient
	model   string
	log     zerolog.Logger
}

func NewChecker(apiKey, model string, timeout time.Duration, log zerolog.Logger) *Checker {
	c := mistral.NewMistralClient(apiKey, "", 1, timeout)
	return &Checker{mistral: c, model: model, log: log}
}

// IsSolved asks the LLM whether the board is the goal state. If the response
// can't be parsed as true/false, it falls back to false and logs a warning.
func (c *Checker) IsSolved(ctx context.Context, gameID string, step int, board [16]int) (bool, error) {
	user := fmt.Sprintf("Текущее состояние поля: %v\nРешена ли игра?", board)

	messages := []mistral.ChatMessage{
		{Role: mistral.RoleSystem, Content: systemPrompt},
		{Role: mistral.RoleUser, Content: user},
	}
	params := mistral.DefaultChatRequestParams
	params.Temperature = 0.0
	params.MaxTokens = 8

	backoff := 2 * time.Second
	var resp *mistral.ChatResponse
	for attempt := 1; attempt <= 5; attempt++ {
		var err error
		start := time.Now()
		resp, err = c.mistral.Chat(c.model, messages, &params)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate_limited") {
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
					return false, ctx.Err()
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
				Err(err).Send()
			return false, fmt.Errorf("%w: %v", ErrMistralUnavailable, err)
		}
		break
	}
	if resp == nil || len(resp.Choices) == 0 {
		return false, fmt.Errorf("%w: empty choices", ErrMistralUnavailable)
	}
	answer := resp.Choices[0].Message.Content

	c.log.Info().
		Str("event", "llm_call").
		Str("gameId", gameID).
		Int("step", step).
		Str("model", c.model).
		Str("prompt", user).
		Str("response", answer).
		Int64("durationMs", dur).
		Send()

	return parseBool(answer, c.log, gameID, step), nil
}

func parseBool(s string, log zerolog.Logger, gameID string, step int) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	switch t {
	case "true":
		return true
	case "false":
		return false
	}
	if strings.HasPrefix(t, "true") {
		return true
	}
	if strings.HasPrefix(t, "false") {
		return false
	}
	log.Warn().
		Str("event", "invalid_move").
		Str("gameId", gameID).
		Int("step", step).
		Str("response", s).
		Msg("checker LLM returned non-boolean answer; defaulting to false")
	return false
}
