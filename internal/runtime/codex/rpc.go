package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RPCMessage is a Codex app-server WebSocket frame. IDs are opaque JSON values:
// server approval requests commonly use numeric IDs, client requests use strings.
type RPCMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("Codex RPC error %d: %s", e.Code, e.Message) }

type RPCClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	next    uint64
	pending map[string]chan RPCMessage
	subs    map[uint64]chan RPCMessage
	done    chan struct{}
	err     error
	once    sync.Once
}

// DialRPC supports private Unix WebSockets and explicitly configured loopback
// WebSockets. The Unix transport is NOT newline-delimited raw JSON.
func DialRPC(ctx context.Context, endpoint string) (*RPCClient, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: nil}
	target := endpoint
	if strings.HasPrefix(endpoint, "unix://") {
		path := strings.TrimPrefix(endpoint, "unix://")
		if !strings.HasPrefix(path, "/") {
			return nil, errors.New("Codex Unix endpoint requires an absolute socket path")
		}
		dialer.NetDialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}
		target = "ws://localhost"
	} else {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "ws" || u.User != nil {
			return nil, errors.New("Codex endpoint must be unix:// or loopback ws://")
		}
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("Codex WebSocket endpoint must be loopback")
		}
	}
	conn, response, err := dialer.DialContext(ctx, target, http.Header{})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("connect Codex app-server: %w", err)
	}
	// thread/resume and thread/read include accumulated history. A long-lived
	// managed thread can exceed 32 MiB before any new work is dispatched. Keep
	// a finite bound on this private/local transport without truncating history.
	conn.SetReadLimit(128 << 20)
	c := &RPCClient{conn: conn, pending: map[string]chan RPCMessage{}, subs: map[uint64]chan RPCMessage{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *RPCClient) Initialize(ctx context.Context, name string) error {
	if err := c.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": name, "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	return c.Notify(ctx, "initialized", map[string]any{})
}

func (c *RPCClient) Call(ctx context.Context, method string, params any, result any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.next++
	id := "oax-" + strconv.FormatUint(c.next, 10)
	ch := make(chan RPCMessage, 1)
	if c.err != nil {
		err = c.err
		c.mu.Unlock()
		return err
	}
	c.pending[strconv.Quote(id)] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, strconv.Quote(id)); c.mu.Unlock() }()
	if err := c.write(ctx, RPCMessage{ID: json.RawMessage(strconv.Quote(id)), Method: method, Params: encoded}); err != nil {
		return err
	}
	select {
	case message := <-ch:
		if message.Error != nil {
			return message.Error
		}
		if result != nil {
			return json.Unmarshal(message.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

func (c *RPCClient) Notify(ctx context.Context, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(ctx, RPCMessage{Method: method, Params: raw})
}

func (c *RPCClient) Respond(ctx context.Context, id json.RawMessage, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.write(ctx, RPCMessage{ID: id, Result: raw})
}

func (c *RPCClient) RespondError(ctx context.Context, id json.RawMessage, code int, message string) error {
	return c.write(ctx, RPCMessage{ID: id, Error: &RPCError{Code: code, Message: message}})
}

func (c *RPCClient) write(ctx context.Context, message RPCMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	if err := c.conn.WriteJSON(message); err != nil {
		c.fail(err)
		return err
	}
	return nil
}

// Subscribe receives both notifications and server requests. Overflow fails the
// connection instead of silently dropping a terminal event or approval request.
func (c *RPCClient) Subscribe() (<-chan RPCMessage, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	ch := make(chan RPCMessage, 2048)
	if c.err != nil {
		close(ch)
	} else {
		c.subs[id] = ch
	}
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if sub, ok := c.subs[id]; ok {
			delete(c.subs, id)
			close(sub)
		}
	}
}
func (c *RPCClient) Done() <-chan struct{} { return c.done }
func (c *RPCClient) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return errors.New("Codex connection closed")
}
func (c *RPCClient) Close() error { c.fail(errors.New("Codex connection closed")); return nil }
func (c *RPCClient) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		for id, ch := range c.subs {
			close(ch)
			delete(c.subs, id)
		}
		close(c.done)
		c.mu.Unlock()
		c.conn.Close()
	})
}
func (c *RPCClient) readLoop() {
	for {
		var m RPCMessage
		if err := c.conn.ReadJSON(&m); err != nil {
			c.fail(err)
			return
		}
		c.mu.Lock()
		if len(m.ID) > 0 && m.Method == "" {
			if ch := c.pending[string(m.ID)]; ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
			c.mu.Unlock()
			continue
		}
		overflow := false
		for _, ch := range c.subs {
			select {
			case ch <- m:
			default:
				overflow = true
			}
		}
		c.mu.Unlock()
		if overflow {
			c.fail(errors.New("Codex notification backlog overflow"))
			return
		}
	}
}
