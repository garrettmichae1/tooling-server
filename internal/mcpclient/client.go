// Package mcpclient speaks newline-delimited JSON-RPC to a headless VectorCraft process.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"
)

// ErrTool means the renderer rejected the call.
var ErrTool = errors.New("tool failed")

// ErrClosed means the renderer pipe closed or the client was shut down.
var ErrClosed = errors.New("renderer closed")

// Client is one VectorCraft MCP session on stdio.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	done   chan struct{}
	mu     sync.Mutex
	seq    int64
	closed bool
}

// Start launches cmd, completes the MCP handshake, and returns a client.
// On failure the process is reaped by the caller via cmd; Start kills it when the handshake fails.
func Start(ctx context.Context, cmd *exec.Cmd) (*Client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	c := &Client{
		cmd:   cmd,
		stdin: stdin,
		lines: make(chan []byte, 16),
		done:  make(chan struct{}),
	}
	go c.drain(stderr)
	go c.read(stdout)
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) drain(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 4096))
	_, _ = io.Copy(io.Discard, r)
}

func (c *Client) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 12<<20)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		select {
		case <-c.done:
			return
		case c.lines <- line:
		}
	}
	close(c.lines)
}

func (c *Client) initialize(ctx context.Context) error {
	_, err := c.rpc(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "edsger-figures", "version": "0.1.0"},
	})
	if err != nil {
		return err
	}
	return c.notify("notifications/initialized")
}

func (c *Client) notify(method string) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method})
}

// Call invokes one MCP tool and returns the raw result object.
func (c *Client) Call(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if len(arguments) == 0 {
		arguments = []byte("{}")
	}
	return c.rpc(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
}

func (c *Client) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	c.seq++
	id := c.seq
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.write(msg); err != nil {
		return nil, ErrClosed
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			return nil, ErrClosed
		case line, ok := <-c.lines:
			if !ok {
				return nil, ErrClosed
			}
			result, matched, err := match(line, id)
			if !matched {
				continue
			}
			if err != nil {
				return nil, ErrTool
			}
			return result, nil
		}
	}
}

func match(line []byte, id int64) (json.RawMessage, bool, error) {
	var env struct {
		ID     *int64          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil || env.ID == nil {
		return nil, false, nil
	}
	if *env.ID != id {
		return nil, false, nil
	}
	if env.Error != nil {
		return nil, true, ErrTool
	}
	var flagged struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(env.Result, &flagged) == nil && flagged.IsError {
		return nil, true, ErrTool
	}
	return env.Result, true, nil
}

func (c *Client) write(msg any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(msg); err != nil {
		return err
	}
	if _, err := c.stdin.Write(buf.Bytes()); err != nil {
		return err
	}
	return nil
}

// Close unblocks readers. The caller reaps the process.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
}
