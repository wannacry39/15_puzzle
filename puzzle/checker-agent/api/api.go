package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"puzzle/checker-agent/llm"
	mcpcli "puzzle/checker-agent/mcp"

	"github.com/rs/zerolog"
	openai "github.com/sashabaranov/go-openai"
)

const maxToolIterations = 10

type Server struct {
	mcp     *mcpcli.Client
	checker *llm.Checker
	log     zerolog.Logger
}

func NewServer(mc *mcpcli.Client, c *llm.Checker, log zerolog.Logger) *Server {
	return &Server{mcp: mc, checker: c, log: log}
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

type invokeResponse struct {
	Solved bool   `json:"solved"`
	GameID string `json:"gameId"`
	Step   int    `json:"step"`
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
		{Role: openai.ChatMessageRoleUser, Content: "Проверь, решена ли текущая игра."},
	}

	for i := 0; i < maxToolIterations; i++ {
		msg, err := s.checker.Chat(ctx, messages, req.GameID, req.Step)
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
			solved := llm.ParseBool(msg.Content)
			s.log.Info().Str("event", "agent_response").Str("gameId", req.GameID).Int("step", req.Step).Bool("solved", solved).Send()
			writeJSON(w, http.StatusOK, invokeResponse{Solved: solved, GameID: req.GameID, Step: req.Step})
			return
		}

		for _, tc := range msg.ToolCalls {
			result, err := s.executeTool(ctx, tc)
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
		}
	}

	s.log.Warn().Str("event", "agent_response").Str("gameId", req.GameID).Int("step", req.Step).Msg("max iterations reached, defaulting to false")
	writeJSON(w, http.StatusOK, invokeResponse{Solved: false, GameID: req.GameID, Step: req.Step})
}

func (s *Server) executeTool(ctx context.Context, tc openai.ToolCall) (string, error) {
	switch tc.Function.Name {
	case "get_state":
		state, err := s.mcp.GetState(ctx)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(state)
		return string(data), nil
	default:
		return fmt.Sprintf(`{"error":"unknown tool: %s"}`, tc.Function.Name), nil
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
