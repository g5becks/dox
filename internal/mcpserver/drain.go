package mcpserver

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// drainTimeout bounds how long the end of input waits for pending replies, so
// a lost reply can never leave the process running after its client is gone.
const drainTimeout = 30 * time.Second

// drainTransport wraps a transport so that the end of input is not reported
// until every call that was read has been answered. The SDK treats the end of
// input as a disconnected peer and cancels all pending calls, which is right
// for an interactive host but drops the replies when requests are piped in
// ("printf ... | dox mcp") or a one-shot client closes stdin after its last
// request.
type drainTransport struct{ inner mcp.Transport }

func (t drainTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &drainConn{
		Connection: conn,
		pending:    map[jsonrpc.ID]struct{}{},
		closed:     make(chan struct{}),
	}, nil
}

type drainConn struct {
	mcp.Connection

	closeOnce sync.Once
	closed    chan struct{}

	mu      sync.Mutex
	pending map[jsonrpc.ID]struct{}
	idle    chan struct{} // non-nil while Read waits for pending to empty
}

func (c *drainConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err == nil {
		if request, ok := msg.(*jsonrpc.Request); ok && request.IsCall() {
			c.mu.Lock()
			c.pending[request.ID] = struct{}{}
			c.mu.Unlock()
		}
		return msg, nil
	}
	if errors.Is(err, io.EOF) {
		c.waitUntilIdle(ctx)
	}
	return nil, err
}

func (c *drainConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	if response, ok := msg.(*jsonrpc.Response); ok {
		c.answered(response.ID)
	}
	return err
}

func (c *drainConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Connection.Close()
}

func (c *drainConn) answered(id jsonrpc.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
	if len(c.pending) == 0 && c.idle != nil {
		close(c.idle)
		c.idle = nil
	}
}

func (c *drainConn) waitUntilIdle(ctx context.Context) {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	idle := make(chan struct{})
	c.idle = idle
	c.mu.Unlock()

	timer := time.NewTimer(drainTimeout)
	defer timer.Stop()
	select {
	case <-idle:
	case <-ctx.Done():
	case <-c.closed:
	case <-timer.C:
	}
}
