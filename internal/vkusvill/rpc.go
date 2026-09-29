package vkusvill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
)

// maxResponseBytes caps a response; a 10-item search is about 7 KiB.
const maxResponseBytes = 1 << 20

type rpcRequest struct {
	JSONRPC string   `json:"jsonrpc"`
	ID      int64    `json:"id"`
	Method  string   `json:"method"`
	Params  toolCall `json:"params"`
}

type toolCall struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  *toolResult     `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// toolResult is an MCP tools/call result. ВкусВилл puts its JSON answer
// into the first text content; structuredContent is preferred when a
// future version sends it.
type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// toolAnswer is the envelope every ВкусВилл tool answers with.
type toolAnswer struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

// callTool runs one tools/call and decodes the tool's data into out. Every
// failure wraps ErrUnavailable; response bodies never go into errors.
func (c *Client) callTool(ctx context.Context, tool string, args, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("%w: %s: rate limit: %w", ErrUnavailable, tool, err)
	}
	data, err := c.post(ctx, tool, args)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, tool, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %s: decode data: %w", ErrUnavailable, tool, err)
	}
	return nil
}

// post sends the request and returns the tool's data.
func (c *Client) post(ctx context.Context, tool string, args any) (json.RawMessage, error) {
	id := c.lastID.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: "tools/call", Params: toolCall{Name: tool, Arguments: args}})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", "dreamer-bot")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err // the URL in it is the fixed endpoint
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, errors.New("response larger than 1 MiB")
	}

	var msg rpcResponse
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "text/event-stream" {
		msg, err = fromEventStream(raw, id)
	} else {
		msg, err = decodeResponse(raw, id)
	}
	if err != nil {
		return nil, err
	}
	return c.toolData(tool, msg)
}

// toolData unwraps the JSON-RPC result down to the tool's data.
func (c *Client) toolData(tool string, msg rpcResponse) (json.RawMessage, error) {
	switch {
	case msg.Error != nil:
		c.log.Debug("vkusvill JSON-RPC error", "tool", tool, "code", msg.Error.Code, "message", msg.Error.Message)
		return nil, fmt.Errorf("JSON-RPC error %d", msg.Error.Code)
	case msg.Result == nil:
		return nil, errors.New("response has neither a result nor an error")
	case msg.Result.IsError:
		return nil, errors.New("tool reported an error")
	}
	payload := []byte(msg.Result.StructuredContent)
	if !isObject(payload) {
		payload = nil
		for _, item := range msg.Result.Content {
			if item.Type == "text" {
				payload = []byte(item.Text)
				break
			}
		}
	}
	if payload == nil {
		return nil, errors.New("result has no content")
	}
	var answer toolAnswer
	if err := json.Unmarshal(payload, &answer); err != nil {
		return nil, fmt.Errorf("decode tool answer: %w", err)
	}
	if !answer.OK {
		code := ""
		if answer.Error != nil {
			code = answer.Error.Code
		}
		return nil, fmt.Errorf("tool error %.40q", code)
	}
	return answer.Data, nil
}

func isObject(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

// decodeResponse reads one JSON-RPC response and checks that it answers
// the request with the given id.
func decodeResponse(raw []byte, id int64) (rpcResponse, error) {
	var msg rpcResponse
	if err := json.Unmarshal(raw, &msg); err != nil {
		return rpcResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if msg.JSONRPC != "2.0" || string(bytes.TrimSpace(msg.ID)) != strconv.FormatInt(id, 10) {
		return rpcResponse{}, errors.New("response does not answer the request")
	}
	return msg, nil
}

// fromEventStream finds the response to request id in a server-sent event
// stream. An event's data is its "data:" lines joined by newlines; events
// end at a blank line. Other messages, such as notifications, are skipped.
func fromEventStream(raw []byte, id int64) (rpcResponse, error) {
	var data [][]byte
	dispatch := func() (rpcResponse, bool) {
		defer func() { data = data[:0] }()
		if len(data) == 0 {
			return rpcResponse{}, false
		}
		msg, err := decodeResponse(bytes.Join(data, []byte("\n")), id)
		return msg, err == nil
	}
	for line := range bytes.Lines(raw) {
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			if msg, ok := dispatch(); ok {
				return msg, nil
			}
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		if string(field) == "data" {
			data = append(data, bytes.TrimPrefix(value, []byte(" ")))
		}
	}
	// A stream may end without the final blank line.
	if msg, ok := dispatch(); ok {
		return msg, nil
	}
	return rpcResponse{}, errors.New("event stream has no response")
}
