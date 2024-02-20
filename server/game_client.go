package main

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

type predictEntry struct {
	created time.Time
	client  *gameClient
	sType   streamType
	dir     assembler.FlowDirection
}

type clientWatch struct {
	mu        sync.RWMutex
	clients   map[uuid.UUID]*gameClient
	predicted map[string]predictEntry
}

type gameClient struct {
	id      uuid.UUID
	mu      sync.RWMutex
	streams map[uuid.UUID]*stream
}

func NewClientWatch() *clientWatch {
	return &clientWatch{
		clients: make(map[uuid.UUID]*gameClient),
	}
}

func (c *clientWatch) newClient() *gameClient {
	gc := &gameClient{
		id:      uuid.New(),
		streams: make(map[uuid.UUID]*stream),
	}
	c.mu.Lock()
	c.clients[gc.id] = gc
	c.mu.Unlock()
	return gc
}
