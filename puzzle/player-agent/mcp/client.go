package mcpcli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type Client struct {
	mcp       *client.Client
	agentName string
	tools     []mcp.Tool
}

func New(ctx context.Context, baseURL, agentName string) (*Client, error) {
	headers := map[string]string{"X-Agent-Name": agentName}
	c, err := client.NewStreamableHttpClient(baseURL, transport.WithHTTPHeaders(headers))
	if err != nil {
		return nil, fmt.Errorf("create mcp client: %w", err)
	}
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("start mcp client: %w", err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: agentName, Version: "v1.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return nil, fmt.Errorf("initialize mcp: %w", err)
	}

	toolsRes, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return &Client{mcp: c, agentName: agentName, tools: toolsRes.Tools}, nil
}

// MCPTools возвращает список тулз, полученных от сервера при старте.
func (c *Client) MCPTools() []mcp.Tool { return c.tools }

func (c *Client) Close() error { return c.mcp.Close() }

type StateResult struct {
	Board  [16]int `json:"board"`
	Step   int     `json:"step"`
	GameID string  `json:"gameId"`
	Error  string  `json:"error,omitempty"`
}

func (c *Client) GetState(ctx context.Context) (*StateResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = "get_state"
	req.Params.Arguments = map[string]any{}
	res, err := c.mcp.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("call get_state: %w", err)
	}
	var out StateResult
	if err := decodeToolJSON(res, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return &out, fmt.Errorf("get_state: %s", out.Error)
	}
	return &out, nil
}

type MoveResult struct {
	Board *[16]int `json:"board,omitempty"`
	Step  int      `json:"step,omitempty"`
	Moved int      `json:"moved,omitempty"`
	Error string   `json:"error,omitempty"`
}

func (c *Client) Move(ctx context.Context, tile int) (*MoveResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = "move"
	req.Params.Arguments = map[string]any{"tile": tile}
	res, err := c.mcp.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("call move: %w", err)
	}
	var out MoveResult
	if err := decodeToolJSON(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func decodeToolJSON(res *mcp.CallToolResult, target any) error {
	if res == nil {
		return fmt.Errorf("nil tool result")
	}
	if len(res.Content) == 0 {
		return fmt.Errorf("empty tool result")
	}
	for _, c := range res.Content {
		tc, ok := c.(mcp.TextContent)
		if !ok {
			continue
		}
		if err := json.Unmarshal([]byte(tc.Text), target); err != nil {
			return fmt.Errorf("decode tool json: %w", err)
		}
		return nil
	}
	return fmt.Errorf("no text content in tool result")
}
