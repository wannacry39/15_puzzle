package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"puzzle/player-agent/llm"
	mcpcli "puzzle/player-agent/mcp"

	"github.com/rs/zerolog"
	openai "github.com/sashabaranov/go-openai"
)

const maxToolIterations = 10

type Server struct {
	mcp    *mcpcli.Client
	player *llm.Player
	log    zerolog.Logger
}

func NewServer(mc *mcpcli.Client, p *llm.Player, log zerolog.Logger) *Server {
	return &Server{mcp: mc, player: p, log: log}
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/invoke", s.handleInvoke)
}

type invokeRequest struct {
	GameID string `json:"gameId"`
	Step   int    `json:"step"`
}

type successResponse struct {
	Tile  int     `json:"tile"`
	Board [16]int `json:"board"`
}

func (s *Server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req invokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}

	ctx := r.Context()
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: llm.SystemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: fmt.Sprintf("Шаг %d. Посмотри на доску и сделай ход.", req.Step)},
	}

	for i := 0; i < maxToolIterations; i++ {
		msg, err := s.player.Chat(ctx, messages, req.GameID, req.Step)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, llm.ErrLLMUnavailable) {
				status = http.StatusBadGateway
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"error": "llm did not call any tool"})
			return
		}

		for _, tc := range msg.ToolCalls {
			result, moveRes, err := s.executeTool(ctx, tc)
			if err != nil {
				s.log.Error().Str("event", "mcp_tool_error").Str("gameId", req.GameID).Int("step", req.Step).Str("tool", tc.Function.Name).Err(err).Send()
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
				return
			}
			s.log.Info().Str("event", "mcp_tool_call").Str("gameId", req.GameID).Int("step", req.Step).Str("tool", tc.Function.Name).Send()

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})

			if tc.Function.Name == "move" && moveRes != nil && moveRes.Error == "" && moveRes.Board != nil {
				s.log.Info().Str("event", "agent_response").Str("gameId", req.GameID).Int("step", req.Step).Int("tile", moveRes.Moved).Send()
				writeJSON(w, http.StatusOK, successResponse{Tile: moveRes.Moved, Board: *moveRes.Board})
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"error": fmt.Sprintf("failed to make a valid move after %d iterations", maxToolIterations),
	})
}

// executeTool выполняет tool call LLM через MCP и возвращает JSON-строку результата.
func (s *Server) executeTool(ctx context.Context, tc openai.ToolCall) (string, *mcpcli.MoveResult, error) {
	switch tc.Function.Name {
	case "get_state":
		state, err := s.mcp.GetState(ctx)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.Marshal(state)
		return string(data), nil, nil

	case "move":
		var args struct {
			Tile int `json:"tile"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return fmt.Sprintf(`{"error":"invalid tile argument: %s"}`, err.Error()), nil, nil
		}
		res, err := s.mcp.Move(ctx, args.Tile)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.Marshal(res)
		return string(data), res, nil

	default:
		return fmt.Sprintf(`{"error":"unknown tool: %s"}`, tc.Function.Name), nil, nil
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
