package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"puzzle/checker-agent/llm"
	mcpcli "puzzle/checker-agent/mcp"

	"github.com/rs/zerolog"
)

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

	state, err := s.mcp.GetState(ctx)
	if err != nil {
		s.log.Error().Str("event", "mcp_tool_error").Str("gameId", req.GameID).Int("step", req.Step).Str("tool", "get_state").Err(err).Send()
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	s.log.Info().Str("event", "mcp_tool_call").Str("gameId", state.GameID).Int("step", state.Step).Str("tool", "get_state").Send()

	solved, err := s.checker.IsSolved(ctx, req.GameID, req.Step, state.Board)
	if err != nil {
		if errors.Is(err, llm.ErrMistralUnavailable) {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": llm.ErrMistralUnavailable.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, invokeResponse{
		Solved: solved,
		GameID: req.GameID,
		Step:   req.Step,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
