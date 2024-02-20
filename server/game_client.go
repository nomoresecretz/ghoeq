package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

const maxPredictTime = 60 * time.Second

type predictEntry struct {
	created time.Time
	client  *gameClient
	sType   streamType
	dir     assembler.FlowDirection
}

type gameClientWatch struct {
	mu        sync.RWMutex
	clients   map[uuid.UUID]*gameClient
	predicted map[string]predictEntry
	ping      chan struct{}
}

type gameClient struct {
	id      uuid.UUID
	mu      sync.RWMutex
	streams map[uuid.UUID]*stream
	epoch   atomic.Uint32
	ping    chan struct{}
}

func NewClientWatch() *gameClientWatch {
	return &gameClientWatch{
		clients: make(map[uuid.UUID]*gameClient),
		ping:    make(chan struct{}),
	}
}

func (c *gameClientWatch) newClient() *gameClient {
	gc := &gameClient{
		id:      uuid.New(),
		streams: make(map[uuid.UUID]*stream),
		ping:    make(chan struct{}),
	}
	c.mu.Lock()
	c.clients[gc.id] = gc
	close(c.ping)
	c.ping = make(chan struct{})
	c.mu.Unlock()
	slog.Debug("new game client tracked", "client", gc)
	return gc
}

func (c *gameClientWatch) Check(p streamPacket) bool {
	c.mu.RLock()
	pr, ok1 := c.predicted[p.stream.key.String()]
	rpr, ok2 := c.predicted[p.stream.key.String()]
	c.mu.RUnlock()

	invalid := time.Now().Add(maxPredictTime)
	if ok1 && pr.created.Before(invalid) {
		p.stream.dir = pr.dir
		p.stream.sType = pr.sType
		p.stream.gameClient = pr.client
		return true
	}
	if ok2 && rpr.created.Before(invalid) {
		p.stream.dir = pr.dir.Reverse()
		p.stream.sType = pr.sType
		p.stream.gameClient = pr.client
		return true
	}
	return false
}

func (c *gameClientWatch) GracefulStop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.ping)
	c.ping = nil
	for _, gc := range c.clients {
		gc.Close()
	}
}

func (c *gameClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.ping)
	c.ping = nil
}

func (c *gameClient) Run(p streamPacket) {
}

func (c *gameClientWatch) Run(p streamPacket) {
	if p.stream.sType == ST_UNKNOWN {
		p.stream.Identify(p.packet)
	}
	if c.Check(p) {
		c.Run(p)
	}
}

// WaitForClient obtains a named client, or can block waiting for first avaliable.
func (c *gameClientWatch) WaitForClient(ctx context.Context, id string) (*gameClient, error) {
	if id == "" {
		c.mu.RLock()
		p := c.ping
		l := len(c.clients)
		c.mu.RUnlock()
		if l == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-p:
			}
			c.mu.RLock()
			for k := range c.clients {
				id = k.String()
			}
			c.mu.RUnlock()
		}
	}

	uId, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid id format: %w", err)
	}

	c.mu.RLock()
	cli, ok := c.clients[uId]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown game client %s", id)
	}

	return cli, nil
}
