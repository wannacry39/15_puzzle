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
	mcp *client.Client
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
	return &Client{mcp: c}, nil
}

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
	if res == nil || len(res.Content) == 0 {
		return nil, fmt.Errorf("empty tool result")
	}
	var out StateResult
	for _, c := range res.Content {
		tc, ok := c.(mcp.TextContent)
		if !ok {
			continue
		}
		if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
			return nil, fmt.Errorf("decode tool json: %w", err)
		}
		if out.Error != "" {
			return &out, fmt.Errorf("get_state: %s", out.Error)
		}
		return &out, nil
	}
	return nil, fmt.Errorf("no text content in tool result")
}
