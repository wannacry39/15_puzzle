# 15-Puzzle Multi-Agent (MCP + Streamable HTTP)

Three-service distributed system that solves the 15-puzzle by orchestrating
two LLM-driven agents (Mistral) over MCP/Streamable-HTTP.

```
Orchestrator (8080) ──REST──▶ Player Agent  (8081)
                  ──REST──▶ Checker Agent (8082)

Player  ──MCP/Streamable HTTP──▶ Orchestrator :8080/mcp  (get_state, move)
Checker ──MCP/Streamable HTTP──▶ Orchestrator :8080/mcp  (get_state)
```

- `orchestrator/` — game state, validation, MCP server (`get_state`, `move`),
  REST endpoints (`POST /game/start`, `GET /game/{id}/result`, `GET /health`),
  and the game loop that drives the agents.
- `player-agent/` — `POST /invoke`. Calls `get_state`, asks Mistral
  (`mistral-large-latest`) for a tile, calls `move` via MCP, retries
  `MAX_MOVE_RETRIES` times on invalid moves.
- `checker-agent/` — `POST /invoke`. Calls `get_state`, asks Mistral
  (`mistral-small-latest`) whether the board is the goal state.

## Run

```bash
cp .env.example .env   # set MISTRAL_API_KEY
docker compose up --build
```

Start a game:

```bash
curl -s -X POST http://localhost:8080/game/start \
  -H 'Content-Type: application/json' \
  -d '{"board":[1,2,3,4,5,6,7,8,9,10,0,12,13,14,11,15]}'
```

Poll the result:

```bash
curl -s http://localhost:8080/game/<gameId>/result | jq
```

## Stack

- Go 1.23
- MCP SDK: `github.com/mark3labs/mcp-go` (Streamable HTTP transport)
- Mistral client: `github.com/gage-technologies/mistral-go`
- Logging: `github.com/rs/zerolog` (JSON to stdout)

## Layout

```
orchestrator/   main.go, api/, mcp/, client/, game/
player-agent/   main.go, api/, mcp/, llm/
checker-agent/  main.go, api/, mcp/, llm/
go.mod / go.sum (single module)
docker-compose.yml, .env.example
```

The repo uses a single Go module, so the three Dockerfiles share
`go.mod`/`go.sum` via the root build context
(`context: .`, `dockerfile: <service>/Dockerfile`).
