package main

import (
	"sync"
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
	rpcClients any
}

type gameClient struct {
	id      uuid.UUID
	mu      sync.RWMutex
	streams map[uuid.UUID]*stream
}

func NewClientWatch() *gameClientWatch {
	return &gameClientWatch{
		clients: make(map[uuid.UUID]*gameClient),
	}
}

func (c *gameClientWatch) newClient() *gameClient {
	gc := &gameClient{
		id:      uuid.New(),
		streams: make(map[uuid.UUID]*stream),
	}
	c.mu.Lock()
	c.clients[gc.id] = gc
	c.mu.Unlock()
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
