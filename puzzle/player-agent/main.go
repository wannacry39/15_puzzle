package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"puzzle/player-agent/api"
	"puzzle/player-agent/llm"
	mcpcli "puzzle/player-agent/mcp"

	"github.com/rs/zerolog"
)

const serviceName = "player-agent"

func main() {
	log := newLogger(serviceName, getEnv("LOG_LEVEL", "info"))

	port := getEnv("PORT", "8081")
	mcpURL := getEnv("ORCHESTRATOR_MCP_URL", "http://orchestrator:8080/mcp")
	apiKey := mustEnv(log, "MISTRAL_API_KEY")
	model := getEnv("MISTRAL_MODEL", "mistral-large-latest")
	timeoutMs := getEnvInt("MISTRAL_TIMEOUT_MS", 20000)
	maxRetries := getEnvInt("MAX_MOVE_RETRIES", 3)

	mc, err := mcpcli.New(context.Background(), mcpURL, serviceName)
	if err != nil {
		log.Fatal().Err(err).Msg("connect to orchestrator MCP")
	}

	player := llm.NewPlayer(apiKey, model, time.Duration(timeoutMs)*time.Millisecond, log)

	srv := api.NewServer(mc, player, maxRetries, log)
	mux := http.NewServeMux()
	srv.Routes(mux)

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

func mustEnv(log zerolog.Logger, k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatal().Str("var", k).Msg("required env var is not set")
	}
	return v
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
