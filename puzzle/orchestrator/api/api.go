package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"puzzle/orchestrator/client"
	"puzzle/orchestrator/game"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Config struct {
	MaxSteps    int
	StepTimeout time.Duration
	StepDelay   time.Duration
}

type Server struct {
	cfg     Config
	reg     *game.Registry
	player  *client.Agent
	checker *client.Agent
	log     zerolog.Logger
}

func NewServer(cfg Config, reg *game.Registry, player, checker *client.Agent, log zerolog.Logger) *Server {
	return &Server{cfg: cfg, reg: reg, player: player, checker: checker, log: log}
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/game/start", s.handleStart)
	mux.HandleFunc("/game/", s.handleGameSubpath)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type startRequest struct {
	Board []int `json:"board"`
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}
	board, err := game.Validate(req.Board)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if !game.IsSolvable(board) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": game.ErrUnsolvable.Error()})
		return
	}

	g := game.NewGame("game-"+uuid.NewString(), board)
	s.reg.Register(g)

	s.log.Info().
		Str("event", "game_start").
		Str("gameId", g.ID()).
		Interface("board", board).
		Msg("game started")

	go s.runLoop(g)

	writeJSON(w, http.StatusOK, map[string]any{
		"gameId": g.ID(),
		"status": "started",
	})
}

func (s *Server) handleGameSubpath(w http.ResponseWriter, r *http.Request) {
	// Path shape: /game/{gameId}/result
	rest := strings.TrimPrefix(r.URL.Path, "/game/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "result" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	g, ok := s.reg.Get(parts[0])
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
		return
	}
	writeJSON(w, http.StatusOK, g.Result())
}

func (s *Server) runLoop(g *game.Game) {
	ctx := context.Background()
	maxSteps := s.cfg.MaxSteps
	stepTimeout := s.cfg.StepTimeout

	for step := 1; step <= maxSteps; step++ {
		if g.IsSolved() {
			break
		}
		s.log.Info().Str("event", "step_start").Str("gameId", g.ID()).Int("step", step).Send()

		// --- Player Agent ---
		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		var pres client.PlayerResponse
		s.log.Info().Str("event", "agent_call").Str("gameId", g.ID()).Int("step", step).Str("agent", s.player.Name).Send()
		err := s.player.Invoke(stepCtx, client.InvokeRequest{GameID: g.ID(), Step: step}, &pres)
		cancel()
		if err != nil {
			s.log.Error().Err(err).Str("event", "game_end").Str("gameId", g.ID()).Int("step", step).Msg("player agent unreachable")
			g.Finish(err.Error())
			return
		}
		if pres.Error != "" {
			s.log.Error().Str("event", "game_end").Str("gameId", g.ID()).Int("step", step).Str("agentError", pres.Error).Send()
			g.Finish(pres.Error)
			return
		}
		if pres.Tile == nil || pres.Board == nil {
			s.log.Error().Str("event", "game_end").Str("gameId", g.ID()).Int("step", step).Msg("player returned malformed response")
			g.Finish("player returned malformed response")
			return
		}
		s.log.Info().Str("event", "agent_response").Str("gameId", g.ID()).Int("step", step).Int("tile", *pres.Tile).Send()

		g.AppendHistory(step, *pres.Tile, *pres.Board)

		if s.cfg.StepDelay > 0 {
			time.Sleep(s.cfg.StepDelay)
		}

		// --- Checker Agent ---
		stepCtx, cancel = context.WithTimeout(ctx, stepTimeout)
		var cres client.CheckerResponse
		s.log.Info().Str("event", "agent_call").Str("gameId", g.ID()).Int("step", step).Str("agent", s.checker.Name).Send()
		err = s.checker.Invoke(stepCtx, client.InvokeRequest{GameID: g.ID(), Step: step}, &cres)
		cancel()
		if err != nil {
			s.log.Error().Err(err).Str("event", "game_end").Str("gameId", g.ID()).Int("step", step).Msg("checker agent unreachable")
			g.Finish(err.Error())
			return
		}
		s.log.Info().Str("event", "agent_response").Str("gameId", g.ID()).Int("step", step).Bool("solved", cres.Solved).Send()
		s.log.Info().Str("event", "step_end").Str("gameId", g.ID()).Int("step", step).Send()

		if cres.Solved {
			g.SetSolved(true)
			s.log.Info().Str("event", "game_end").Str("gameId", g.ID()).Int("step", step).Bool("solved", true).Send()
			g.Finish("")
			return
		}
	}

	s.log.Warn().Str("event", "game_end").Str("gameId", g.ID()).Bool("solved", false).Msg("max steps exceeded")
	g.Finish(errMaxSteps.Error())
}

var errMaxSteps = errors.New("max steps exceeded")

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
