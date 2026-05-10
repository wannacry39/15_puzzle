package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"puzzle/player-agent/llm"
	mcpcli "puzzle/player-agent/mcp"

	"github.com/rs/zerolog"
)

type Server struct {
	mcp        *mcpcli.Client
	player     *llm.Player
	maxRetries int
	log        zerolog.Logger
}

func NewServer(mc *mcpcli.Client, p *llm.Player, maxRetries int, log zerolog.Logger) *Server {
	return &Server{mcp: mc, player: p, maxRetries: maxRetries, log: log}
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

type failureResponse struct {
	Error             string `json:"error"`
	LastAttemptedTile *int   `json:"lastAttemptedTile,omitempty"`
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
		s.log.Error().Str("event", "mcp_tool_error").Str("gameId", req.GameID).Int("step", req.Step).Err(err).Str("tool", "get_state").Send()
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	s.log.Info().Str("event", "mcp_tool_call").Str("gameId", state.GameID).Int("step", state.Step).Str("tool", "get_state").Send()

	board := state.Board
	var lastTile int

	for attempt := 1; attempt <= s.maxRetries; attempt++ {
		tile, err := s.player.ChooseTile(ctx, req.GameID, req.Step, board, lastTile)
		if err != nil {
			if errors.Is(err, llm.ErrMistralUnavailable) {
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": llm.ErrMistralUnavailable.Error()})
				return
			}
			// unparsable → log and treat the attempt as failed
			s.log.Warn().Str("event", "invalid_move").Str("gameId", req.GameID).Int("step", req.Step).Err(err).Send()
			s.log.Info().Str("event", "retry").Str("gameId", req.GameID).Int("step", req.Step).Int("attempt", attempt).Send()
			continue
		}
		lastTile = tile

		mv, err := s.mcp.Move(ctx, tile)
		if err != nil {
			s.log.Error().Str("event", "mcp_tool_error").Str("gameId", req.GameID).Int("step", req.Step).Str("tool", "move").Err(err).Send()
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		s.log.Info().
			Str("event", "mcp_tool_call").
			Str("gameId", req.GameID).
			Int("step", req.Step).
			Str("tool", "move").
			Int("input_tile", tile).
			Str("toolError", mv.Error).
			Send()

		if mv.Error == "" && mv.Board != nil {
			writeJSON(w, http.StatusOK, successResponse{Tile: tile, Board: *mv.Board})
			return
		}

		// Invalid move → update prompt context for next retry
		s.log.Warn().Str("event", "invalid_move").Str("gameId", req.GameID).Int("step", req.Step).Int("tile", tile).Str("reason", mv.Error).Send()
		if strings.Contains(mv.Error, "does not exist") {
			// keep prior board
		} else if mv.Board != nil {
			board = *mv.Board
		}
		s.log.Info().Str("event", "retry").Str("gameId", req.GameID).Int("step", req.Step).Int("attempt", attempt).Send()
	}

	tile := lastTile
	writeJSON(w, http.StatusOK, failureResponse{
		Error:             fmt.Sprintf("failed to make a valid move after %d attempts", s.maxRetries),
		LastAttemptedTile: &tile,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

