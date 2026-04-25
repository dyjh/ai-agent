package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

// Client is a small public-API client for the local Agent backend.
type Client struct {
	BaseURL   string
	HTTP      *http.Client
	Timeout   time.Duration
	UserAgent string
}

// Event mirrors the public websocket event shape without depending on backend packages.
type Event struct {
	Type       string         `json:"type"`
	RunID      string         `json:"run_id,omitempty"`
	StepID     string         `json:"step_id,omitempty"`
	ApprovalID string         `json:"approval_id,omitempty"`
	Content    string         `json:"content,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	Raw        map[string]any `json:"-"`
}

// New returns a client with conservative defaults.
func New(baseURL string) Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return Client{
		BaseURL:   baseURL,
		HTTP:      &http.Client{Timeout: 20 * time.Second},
		Timeout:   20 * time.Second,
		UserAgent: "local-agent-tui/0.1",
	}
}

// Request sends a JSON request and decodes the response as a generic map.
func (c Client) Request(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", c.UserAgent)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	if resp.StatusCode >= 400 {
		if decoded != nil {
			if msg, ok := decoded["message"].(string); ok {
				return decoded, fmt.Errorf("%s", msg)
			}
			if msg, ok := decoded["error"].(string); ok {
				return decoded, fmt.Errorf("%s", msg)
			}
		}
		return decoded, fmt.Errorf("request failed: %s", resp.Status)
	}
	return decoded, nil
}

func (c Client) CreateConversation(ctx context.Context) (string, error) {
	resp, err := c.Request(ctx, http.MethodPost, "/v1/conversations", map[string]any{"title": "TUI chat"})
	if err != nil {
		return "", err
	}
	id, _ := resp["id"].(string)
	if id == "" {
		return "", fmt.Errorf("conversation id missing")
	}
	return id, nil
}

func (c Client) PostMessage(ctx context.Context, conversationID, content string) (map[string]any, error) {
	return c.Request(ctx, http.MethodPost, "/v1/conversations/"+conversationID+"/messages", map[string]any{"content": content})
}

func (c Client) PendingApprovals(ctx context.Context) (map[string]any, error) {
	return c.Request(ctx, http.MethodGet, "/v1/approvals/pending", nil)
}

func (c Client) Approve(ctx context.Context, approvalID string) (map[string]any, error) {
	return c.Request(ctx, http.MethodPost, "/v1/approvals/"+approvalID+"/approve", map[string]any{})
}

func (c Client) Reject(ctx context.Context, approvalID, reason string) (map[string]any, error) {
	return c.Request(ctx, http.MethodPost, "/v1/approvals/"+approvalID+"/reject", map[string]any{"reason": reason})
}

func (c Client) Runs(ctx context.Context) (map[string]any, error) {
	return c.Request(ctx, http.MethodGet, "/v1/runs?limit=20", nil)
}

func (c Client) RunSteps(ctx context.Context, runID string) (map[string]any, error) {
	return c.Request(ctx, http.MethodGet, "/v1/runs/"+runID+"/steps", nil)
}

func (c Client) MemorySearch(ctx context.Context, query string) (map[string]any, error) {
	return c.Request(ctx, http.MethodPost, "/v1/memory/search", map[string]any{"query": query, "limit": 5})
}

func (c Client) KBAnswer(ctx context.Context, kbID, question string) (map[string]any, error) {
	return c.Request(ctx, http.MethodPost, "/v1/kbs/"+kbID+"/answer", map[string]any{"question": question, "query": question, "mode": "no_citation_no_answer", "top_k": 5})
}

func (c Client) EvalRuns(ctx context.Context) (map[string]any, error) {
	return c.Request(ctx, http.MethodGet, "/v1/evals/runs?limit=10", nil)
}

// ChatSocket opens the public conversation WebSocket.
func (c Client) ChatSocket(ctx context.Context, conversationID string) (*websocket.Conn, error) {
	url := strings.Replace(c.BaseURL, "http://", "ws://", 1)
	url = strings.Replace(url, "https://", "wss://", 1)
	conn, _, err := websocket.Dial(ctx, url+"/v1/conversations/"+conversationID+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"user-agent": []string{c.UserAgent}},
	})
	return conn, err
}

func SendUserMessage(ctx context.Context, conn *websocket.Conn, content string) error {
	return writeWS(ctx, conn, map[string]any{"type": "user.message", "content": content})
}

func SendApprovalResponse(ctx context.Context, conn *websocket.Conn, approvalID string, approved bool) error {
	return writeWS(ctx, conn, map[string]any{"type": "approval.respond", "approval_id": approvalID, "approved": approved})
}

func writeWS(ctx context.Context, conn *websocket.Conn, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, raw)
}

func ReadEvent(ctx context.Context, conn *websocket.Conn) (Event, error) {
	_, raw, err := conn.Read(ctx)
	if err != nil {
		return Event{}, err
	}
	var event Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return Event{}, err
	}
	_ = json.Unmarshal(raw, &event.Raw)
	return event, nil
}
