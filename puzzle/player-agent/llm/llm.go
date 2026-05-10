package llm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	mistral "github.com/gage-technologies/mistral-go"
	"github.com/rs/zerolog"
)

const systemPrompt = `Ты — игрок в игру «Пятнашки» (15 Puzzle).

Поле 4×4, числа от 1 до 15 и пустая клетка (0).
Победное состояние: [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,0]

Тебе передаётся текущее состояние поля в виде плоского массива из 16 чисел.
Индексы идут слева направо, сверху вниз (0–15).
Пустая клетка обозначена 0.

Ты можешь сдвинуть плитку только если она стоит рядом с пустой клеткой
(выше, ниже, левее или правее по сетке 4×4).

Твоя задача: выбрать номер одной плитки для сдвига.

Отвечай ТОЛЬКО числом — номером плитки которую нужно сдвинуть.
Никаких пояснений, только число. Например: 15`

// ErrMistralUnavailable is returned when the Mistral API call fails.
var ErrMistralUnavailable = errors.New("mistral api unavailable")

// ErrUnparsable is returned when the model response cannot be parsed as a tile number.
var ErrUnparsable = errors.New("llm response is not a valid tile number")

type Player struct {
	mistral *mistral.MistralClient
	model   string
	timeout time.Duration
	log     zerolog.Logger
}

func NewPlayer(apiKey, model string, timeout time.Duration, log zerolog.Logger) *Player {
	c := mistral.NewMistralClient(apiKey, "", 1, timeout)
	return &Player{mistral: c, model: model, timeout: timeout, log: log}
}

// ChooseTile asks the LLM for a tile number. invalidPrior, if non-zero, names a
// tile that the previous attempt selected and which the orchestrator rejected.
func (p *Player) ChooseTile(ctx context.Context, gameID string, step int, board [16]int, invalidPrior int) (int, error) {
	user := fmt.Sprintf("Текущее состояние поля: %v\nВыбери номер плитки для сдвига.", board)
	if invalidPrior != 0 {
		user += fmt.Sprintf("\nХод %d недопустим, эта плитка не стоит рядом с пустой клеткой. Выбери другую.", invalidPrior)
	}

	messages := []mistral.ChatMessage{
		{Role: mistral.RoleSystem, Content: systemPrompt},
		{Role: mistral.RoleUser, Content: user},
	}
	params := mistral.DefaultChatRequestParams
	params.Temperature = 0.2
	params.MaxTokens = 16

	backoff := 2 * time.Second
	for attempt := 1; attempt <= 5; attempt++ {
		start := time.Now()
		resp, err := p.mistral.Chat(p.model, messages, &params)
		dur := time.Since(start).Milliseconds()
		if err != nil {
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "rate_limited") {
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
					return 0, ctx.Err()
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
			return 0, fmt.Errorf("%w: %v", ErrMistralUnavailable, err)
		}
		if len(resp.Choices) == 0 {
			return 0, fmt.Errorf("%w: empty choices", ErrMistralUnavailable)
		}
		answer := resp.Choices[0].Message.Content
		p.log.Info().
			Str("event", "llm_call").
			Str("gameId", gameID).
			Int("step", step).
			Str("model", p.model).
			Str("prompt", user).
			Str("response", answer).
			Int64("durationMs", dur).
			Send()
		tile, ok := parseTile(answer)
		if !ok {
			return 0, fmt.Errorf("%w: %q", ErrUnparsable, answer)
		}
		return tile, nil
	}
	return 0, fmt.Errorf("%w: rate limit exceeded after retries", ErrMistralUnavailable)
}

var tileRe = regexp.MustCompile(`-?\d+`)

func parseTile(s string) (int, bool) {
	m := tileRe.FindString(s)
	if m == "" {
		return 0, false
	}
	n, err := strconv.Atoi(m)
	if err != nil {
		return 0, false
	}
	if n < 1 || n > 15 {
		return 0, false
	}
	return n, true
}
