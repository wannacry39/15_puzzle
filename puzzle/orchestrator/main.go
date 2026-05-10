package main

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"puzzle/orchestrator/api"
	"puzzle/orchestrator/client"
	"puzzle/orchestrator/game"
	mcpsrv "puzzle/orchestrator/mcp"

	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

const serviceName = "orchestrator"

func main() {
	log := newLogger(serviceName, getEnv("LOG_LEVEL", "info"))

	port := getEnv("PORT", "8080")
	playerURL := getEnv("PLAYER_URL", "http://player-agent:8081")
	checkerURL := getEnv("CHECKER_URL", "http://checker-agent:8082")
	maxSteps := getEnvInt("MAX_STEPS", 200)
	stepTimeoutMs := getEnvInt("STEP_TIMEOUT_MS", 30000)
	stepDelayMs := getEnvInt("STEP_DELAY_MS", 0)

	reg := game.NewRegistry()

	mcpServer := mcpsrv.Build(reg)
	streamable := server.NewStreamableHTTPServer(mcpServer)

	playerClient := client.New("player-agent", playerURL, time.Duration(stepTimeoutMs)*time.Millisecond)
	checkerClient := client.New("checker-agent", checkerURL, time.Duration(stepTimeoutMs)*time.Millisecond)

	apiServer := api.NewServer(api.Config{
		MaxSteps:    maxSteps,
		StepTimeout: time.Duration(stepTimeoutMs) * time.Millisecond,
		StepDelay:   time.Duration(stepDelayMs) * time.Millisecond,
	}, reg, playerClient, checkerClient, log)

	mux := http.NewServeMux()
	apiServer.Routes(mux)
	mux.Handle("/mcp", streamable)
	mux.Handle("/mcp/", streamable)

	addr := ":" + port
	log.Info().Str("event", "service_start").Str("addr", addr).Send()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal().Err(err).Msg("http server failed")
	}
}

func getEnv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func newLogger(service, level string) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFieldName = "timestamp"
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	return zerolog.New(os.Stdout).
		Level(lvl).
		With().
		Timestamp().
		Str("service", service).
		Logger()
}
