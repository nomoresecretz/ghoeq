package game_client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq/server/common"
	"github.com/nomoresecretz/ghoeq/server/stream"
)

type GameClientWatch struct {
	mu        sync.RWMutex
	clients   map[uuid.UUID]*GameClient
	predicted streamPredict
	ping      chan struct{}
	Decoder   common.OpDecoder
	charMap   map[string]*GameClient
	acctMap   map[string]*GameClient
	db        *DB
}

func NewClientWatch() (*GameClientWatch, error) {
	db, err := newDB()
	if err != nil {
		return nil, fmt.Errorf("failed to create db: %w", err)
	}

	return &GameClientWatch{
		clients:   make(map[uuid.UUID]*GameClient),
		ping:      make(chan struct{}),
		predicted: make(streamPredict),
		charMap:   make(map[string]*GameClient),
		acctMap:   make(map[string]*GameClient),
		db:        db,
	}, nil
}

func (c *GameClientWatch) newClient() *GameClient {
	gc := New(c)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[gc.ID] = gc
	c.LockedPing()

	slog.Debug("new game client tracked", "client", gc.ID)

	return gc
}

func (c *GameClientWatch) LockedPing() {
	ping := c.ping
	c.ping = make(chan struct{})

	close(ping)
}

func (c *GameClientWatch) CheckFull(p *stream.StreamPacket, pr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || pr.created.After(invalid) {
		return false
	}

	p.Stream.Dir = pr.dir
	p.Stream.Type = pr.sType
	p.Stream.GameClient = pr.client

	if pr.client == nil {
		return true
	}

	pr.client.AddStream(p.Stream)

	return true
}

func (c *GameClientWatch) CheckReverse(p *stream.StreamPacket, rpr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || rpr.created.After(invalid) {
		return false
	}

	p.Stream.Dir = rpr.dir.Reverse()
	p.Stream.Type = rpr.sType
	p.Stream.GameClient = rpr.client

	if rpr.client == nil {
		return true
	}

	rpr.client.AddStream(p.Stream)

	return true
}

func (c *GameClientWatch) CheckHalf(p *stream.StreamPacket) bool {
	testKey := fmt.Sprintf("dst-%s:%s", p.Stream.Net.Dst().String(), p.Stream.Port.Dst().String())
	testKeyR := fmt.Sprintf("src-%s:%s", p.Stream.Net.Src().String(), p.Stream.Port.Src().String())

	c.mu.RLock()
	prF, okF := c.predicted[testKey]
	prB, okB := c.predicted[testKeyR]
	c.mu.RUnlock()

	if okF {
		p.Stream.Dir = prF.dir
		p.Stream.Type = prF.sType
		p.Stream.GameClient = prF.client

		if prF.client == nil {
			return true
		}

		prF.client.AddStream(p.Stream)

		return true
	}

	if okB {
		p.Stream.Dir = prB.dir
		p.Stream.Type = prB.sType
		p.Stream.GameClient = prB.client

		if prB.client == nil {
			return true
		}

		prB.client.AddStream(p.Stream)

		return true
	}

	return false
}

func (c *GameClientWatch) Check(p *stream.StreamPacket) bool {
	c.mu.RLock()
	pr, ok1 := c.predicted[p.Stream.Key.String()]
	rv := p.Stream.Key.Reverse()
	rpr, ok2 := c.predicted[rv.String()]
	c.mu.RUnlock()

	if r := c.CheckFull(p, pr, ok1); r {
		return r
	}

	if r := c.CheckReverse(p, rpr, ok2); r {
		return r
	}

	if r := c.CheckHalf(p); r {
		return r
	}

	return false
}

func (c *GameClientWatch) newPredictClient(p *stream.StreamPacket) {
	rv := p.Stream.Key.Reverse()
	cli := c.newClient()
	slog.Info("unknown/new client thread seen, adding client", "client", cli.ID, "streamtype", p.Stream.Type.String())
	p.Stream.GameClient = cli
	cli.AddStream(p.Stream)
	c.predicted[rv.String()] = NewPredict(cli, p.Stream.Type, p.Stream.Dir.Reverse())
}

func (gw *GameClientWatch) GracefulStop() {
	for _, gc := range gw.clients {
		gc.Close()
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()

	gw.ClosePing()
}

func (c *GameClientWatch) charMatch(n string) *GameClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, gc := range c.clients {
		if gc.clientCharacter == n {
			return gc
		}
	}

	return nil
}

func (c *GameClientWatch) ClosePing() {
	ping := c.ping
	c.ping = nil

	if ping != nil {
		close(ping)
	}
}

func (gw *GameClientWatch) matchChar(n string) *GameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.charMap[n]; ok {
		return c
	}

	return nil
}

func (gw *GameClientWatch) matchAcct(n string) *GameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.acctMap[n]; ok {
		return c
	}

	return nil
}

func (gw *GameClientWatch) RegisterCharacter(c *GameClient) error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	cc, ok := gw.charMap[c.clientCharacter]
	if ok {
		if cc != c {
			return fmt.Errorf("character already registered to a different client")
		}
	}

	gw.charMap[c.clientCharacter] = c

	return nil
}

func (gw *GameClientWatch) RegisterAcct(c *GameClient) error {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	cc, ok := gw.acctMap[c.clientSessionID]
	if ok {
		if cc != c {
			return fmt.Errorf("account already registered to a different client")
		}
	}

	gw.acctMap[c.clientSessionID] = c

	return nil
}

func (c *GameClientWatch) Run(ctx context.Context, p *stream.StreamPacket) error {
	if c.Check(p) {
		slog.Debug("success matching unknown flow", "stream", p.Stream.Key.String())

		if err := p.Stream.GameClient.Run(ctx, p); err != nil {
			return err
		}

		return nil
	}

	c.Check(p)

	if p.Stream.Type == stream.ST_UNKNOWN {
		p.Stream.Identify(p.Packet)
	}

	if c.Check(p) {
		slog.Debug("fallback matching unknown flow", "stream", p.Stream.Key.String())

		if err := p.Stream.GameClient.Run(ctx, p); err != nil {
			return err
		}
	}

	switch p.Stream.Type {
	case stream.ST_WORLD, stream.ST_LOGIN: // Wait for a zone event at minimum.
		c.newPredictClient(p)

		return p.Stream.GameClient.Run(ctx, p)
	}

	return nil
}

func (c *GameClientWatch) getFirstClient() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for k := range c.clients {
		return k.String()
	}

	return ""
}

// WaitForClient obtains a named client, or can block waiting for first available.
func (c *GameClientWatch) WaitForClient(ctx context.Context, id string) (*GameClient, error) {
	if id == "" {
		c.mu.RLock()
		p := c.ping
		l := len(c.clients)
		c.mu.RUnlock()

		switch l {
		case 0:
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-p:
			}

			id = c.getFirstClient()
		case 1:
			id = c.getFirstClient()
		default:
			return nil, fmt.Errorf("more than 1 game client to pick from, please select one to follow")
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
