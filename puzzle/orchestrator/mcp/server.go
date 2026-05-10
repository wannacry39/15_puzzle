package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"puzzle/orchestrator/game"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Build registers get_state and move tools on the given MCP server.
func Build(reg *game.Registry) *server.MCPServer {
	s := server.NewMCPServer(
		"15-puzzle-orchestrator",
		"v1.0.0",
		server.WithToolCapabilities(true),
	)

	s.AddTool(
		mcp.NewTool(
			"get_state",
			mcp.WithDescription("Returns the current board, step counter, and gameId of the active game."),
		),
		makeGetState(reg),
	)

	s.AddTool(
		mcp.NewTool(
			"move",
			mcp.WithDescription("Slides the given tile into the empty cell. Returns the new board and step on success, or an error if the tile is invalid or not adjacent to the empty cell."),
			mcp.WithNumber(
				"tile",
				mcp.Required(),
				mcp.Description("Tile number to slide (1..15)."),
			),
		),
		makeMove(reg),
	)

	return s
}

func makeGetState(reg *game.Registry) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		g := reg.Active()
		if g == nil {
			return jsonError("no active game"), nil
		}
		board, step, _, _, _ := g.Snapshot()

		from := callerFromHeader(req.Header)
		g.AppendTrace(game.TraceEntry{
			Step: step + 1,
			From: from,
			To:   "orchestrator",
			Tool: "get_state",
		})

		out := map[string]any{
			"board":  board,
			"step":   step,
			"gameId": g.ID(),
		}
		return jsonResult(out), nil
	}
}

func makeMove(reg *game.Registry) server.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		g := reg.Active()
		if g == nil {
			return jsonError("no active game"), nil
		}

		var tile int
		args := req.GetArguments()
		switch v := args["tile"].(type) {
		case float64:
			tile = int(v)
		case int:
			tile = v
		case string:
			if _, err := fmt.Sscanf(v, "%d", &tile); err != nil {
				return jsonError(fmt.Sprintf("tile must be a number, got %q", v)), nil
			}
		default:
			return jsonError("tile must be a number"), nil
		}

		from := callerFromHeader(req.Header)

		preStep := g.Step()
		newBoard, newStep, err := g.Move(tile)
		if err != nil {
			g.AppendTrace(game.TraceEntry{
				Step:  preStep + 1,
				From:  from,
				To:    "orchestrator",
				Tool:  "move",
				Input: map[string]any{"tile": tile},
			})
			// Per spec: "tile X does not exist" has no board; adjacency error includes board.
			out := map[string]any{"error": err.Error()}
			if isAdjacencyErr(err) {
				out["board"] = newBoard
			}
			return jsonResult(out), nil
		}

		g.AppendTrace(game.TraceEntry{
			Step:  newStep,
			From:  from,
			To:    "orchestrator",
			Tool:  "move",
			Input: map[string]any{"tile": tile},
		})

		out := map[string]any{
			"board": newBoard,
			"step":  newStep,
			"moved": tile,
		}
		return jsonResult(out), nil
	}
}

func isAdjacencyErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is not adjacent")
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return mcp.NewToolResultText(string(data))
}

func jsonError(msg string) *mcp.CallToolResult {
	data, _ := json.Marshal(map[string]any{"error": msg})
	return mcp.NewToolResultText(string(data))
}

// callerFromHeader returns the agent name derived from the X-Agent-Name HTTP header.
func callerFromHeader(h http.Header) string {
	if h == nil {
		return "unknown"
	}
	if v := h.Get("X-Agent-Name"); v != "" {
		return v
	}
	return "unknown"
}
