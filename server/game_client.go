package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq/common/eqStruct"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

const maxPredictTime = 60 * time.Second

type predictEntry struct {
	created time.Time
	client  *gameClient
	sType   streamType
	dir     assembler.FlowDirection
}

type streamPredict map[string]predictEntry

type gameClientWatch struct {
	mu        sync.RWMutex
	clients   map[uuid.UUID]*gameClient
	predicted streamPredict
	ping      chan struct{}
	decoder   opDecoder
	charMap   map[string]*gameClient
	acctMap   map[string]*gameClient
}

type gameClient struct {
	id              uuid.UUID
	mu              sync.RWMutex
	streams         map[assembler.Key]*stream
	epoch           atomic.Uint32
	ping            chan struct{}
	decoder         opDecoder
	parent          *gameClientWatch
	clientServer    string // game server name.
	clientSessionID string // Assigned by server.
	clientCharacter string // game client character.
}

func NewPredict(gc *gameClient, s streamType, dir assembler.FlowDirection) predictEntry {
	return predictEntry{
		created: now(),
		client:  gc,
		sType:   s,
		dir:     dir,
	}
}

func NewClientWatch() *gameClientWatch {
	return &gameClientWatch{
		clients:   make(map[uuid.UUID]*gameClient),
		ping:      make(chan struct{}),
		predicted: make(streamPredict),
		charMap:   make(map[string]*gameClient),
		acctMap:   make(map[string]*gameClient),
	}
}

func (c *gameClientWatch) newClient() *gameClient {
	gc := &gameClient{
		id:      uuid.New(),
		streams: make(map[assembler.Key]*stream),
		ping:    make(chan struct{}),
		decoder: c.decoder,
		parent:  c,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[gc.id] = gc
	close(c.ping)
	c.ping = make(chan struct{})
	slog.Debug("new game client tracked", "client", gc.id)

	return gc
}

func (c *gameClient) AddStream(s *stream) {
	slog.Debug("gameClient stream added")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streams[s.key] = s
	c.LockedPing()
}

func (c *gameClient) LockedPing() {
	close(c.ping)
	c.ping = make(chan struct{})
}

func (c *gameClientWatch) CheckFull(p StreamPacket, pr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || pr.created.After(invalid) {
		return false
	}
	p.stream.dir = pr.dir
	p.stream.sType = pr.sType
	p.stream.gameClient = pr.client

	if pr.client == nil {
		return true
	}

	pr.client.AddStream(p.stream)

	return true
}

func (c *gameClientWatch) CheckReverse(p StreamPacket, rpr predictEntry, b bool) bool {
	invalid := time.Now().Add(maxPredictTime)
	if !b || rpr.created.After(invalid) {
		return false
	}

	p.stream.dir = rpr.dir.Reverse()
	p.stream.sType = rpr.sType
	p.stream.gameClient = rpr.client
	if rpr.client == nil {
		return true
	}
	rpr.client.AddStream(p.stream)
	return true
}

func (c *gameClientWatch) CheckHalf(p StreamPacket) bool {
	testKey := fmt.Sprintf("dst-%s:%s", p.stream.net.Dst().String(), p.stream.port.Dst().String())
	testKeyR := fmt.Sprintf("src-%s:%s", p.stream.net.Src().String(), p.stream.port.Src().String())

	c.mu.RLock()
	prF, okF := c.predicted[testKey]
	prB, okB := c.predicted[testKeyR]
	c.mu.RUnlock()

	if okF {
		p.stream.dir = prF.dir
		p.stream.sType = prF.sType
		p.stream.gameClient = prF.client

		if prF.client == nil {
			return true
		}

		prF.client.AddStream(p.stream)

		return true
	}

	if okB {
		p.stream.dir = prB.dir
		p.stream.sType = prB.sType
		p.stream.gameClient = prB.client

		if prB.client == nil {
			return true
		}

		prB.client.AddStream(p.stream)

		return true
	}

	return false
}

func (c *gameClientWatch) Check(p StreamPacket) bool {
	c.mu.RLock()
	pr, ok1 := c.predicted[p.stream.key.String()]
	rv := p.stream.key.Reverse()
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

func (c *gameClientWatch) newPredictClient(p StreamPacket) {
	rv := p.stream.key.Reverse()
	cli := c.newClient()
	slog.Info("unknown/new client thread seen, adding client", "client", cli.id, "streamtype", p.stream.sType.String())
	p.stream.gameClient = cli
	cli.AddStream(p.stream)
	c.predicted[rv.String()] = NewPredict(cli, p.stream.sType, p.stream.dir.Reverse())
}

func (gw *gameClientWatch) GracefulStop() {
	for _, gc := range gw.clients {
		gc.Close()
	}
	gw.mu.Lock()
	defer gw.mu.Unlock()
	close(gw.ping)
	gw.ping = nil
}

func (c *gameClientWatch) charMatch(n string) *gameClient {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, gc := range c.clients {
		if gc.clientCharacter == n {
			return gc
		}
	}

	return nil
}

func (c *gameClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.ping)
	c.ping = nil
	c.parent.mu.Lock()
	delete(c.parent.charMap, c.clientCharacter)
	c.parent.mu.Unlock()
}

func (c *gameClient) Run(p StreamPacket) error {
	op := c.decoder.GetOp(p.opCode)
	switch {
	case op == "":
		slog.Debug("game client unknown opcode", "opcode", p.opCode.String())
	case op == "OP_PlayEverquestRequest":
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		ply := &eqStruct.PlayRequest{}
		if err := ply.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		Key := fmt.Sprintf("%s:%d", ply.IP, 9000)
		fKey, rKey := "dst-"+Key, "src-"+Key
		c.parent.mu.Lock()
		c.parent.predicted[fKey] = NewPredict(c, ST_WORLD, assembler.DirClientToServer)
		c.parent.predicted[rKey] = NewPredict(c, ST_WORLD, assembler.DirServerToClient)
		c.parent.mu.Unlock()

	case op == "OP_LogServer" && p.stream.dir == assembler.DirServerToClient:
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		ls := eqStruct.LogServer{}
		if err := ls.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		if c.clientServer == "" {
			c.clientServer = ls.ShortName
		}

	case op == "OP_EnterWorld":
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		ew := eqStruct.EnterWorld{}
		if err := ew.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		if c.matchChar(ew.Name) {
			return nil
		}

		if c.clientCharacter == "" {
			c.clientCharacter = ew.Name
		}
		if c.matchChar(ew.Name) {
			return nil
		}

		if err := c.parent.RegisterCharacter(c); err != nil {
			return err
		}

	case op == "OP_ZoneServerInfo":
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		zi := &eqStruct.ZoneServerInfo{}
		if err := zi.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		slog.Debug("zoning event detected", "client", c.id)
		Key := fmt.Sprintf("%s:%d", zi.IP, zi.Port)
		fKey, rKey := "dst-"+Key, "src-"+Key
		c.parent.mu.Lock()
		c.parent.predicted[fKey] = NewPredict(c, ST_ZONE, assembler.DirClientToServer)
		c.parent.predicted[rKey] = NewPredict(c, ST_ZONE, assembler.DirServerToClient)
		c.parent.mu.Unlock()

	case op == "OP_ZoneEntry" && p.stream.dir == assembler.DirServerToClient:
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		zs := eqStruct.ServerZoneEntry{}
		if err := zs.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		if c.matchChar(zs.Name) {
			return nil
		}

		if err := c.parent.RegisterCharacter(c); err != nil {
			return err
		}

	case op == "OP_SendLoginInfo":
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		li := eqStruct.LoginInfo{}
		if err := li.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		if c.matchAcct(li.Account) {
			return nil
		}
		
		c.clientSessionID = li.Account
		if err := c.parent.RegisterAcct(c); err != nil {
			return err
		}

	case op == "OP_LoginAccepted":
		slog.Debug("game track", "opCode", op, "dir", p.stream.dir.String())
		li := eqStruct.LoginAccepted{}
		if err := li.Unmarshal(p.packet.Payload); err != nil {
			return err
		}

		if c.matchAcct(li.Account) {
			return nil
		}

		c.clientSessionID = li.Account
		if err := c.parent.RegisterAcct(c); err != nil {
			return err
		}

	}

	return nil
}

func (gw *gameClientWatch) matchChar(n string) *gameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.charMap[n]; ok {
		return c
	}

	return nil
}

func (gw *gameClientWatch) matchAcct(n string) *gameClient {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	// TODO: be server aware.
	if c, ok := gw.acctMap[n]; ok {
		return c
	}

	return nil
}

func (gw *gameClientWatch) RegisterCharacter(c *gameClient) error {
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

func (gw *gameClientWatch) RegisterAcct(c *gameClient) error {
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

func (c *gameClient) matchChar(n string) bool {
	gc := c.parent.matchChar(n)
	if gc == nil || gc == c {
		return false
	}
	slog.Info("matched extra client by charName, reducing", "name", n)

	c.reduce(gc)

	return true
}

func (c *gameClient) matchAcct(n string) bool {
	gc := c.parent.matchAcct(n)
	if gc == nil || gc == c {
		return false
	}
	slog.Info("matched extra client by account, reducing", "name", n)

	c.reduce(gc)
	return true
}

func (c *gameClient) reduce(gc *gameClient) {
	gc.mu.Lock()
	c.mu.Lock()
	for _, s := range c.streams {
		s.gameClient = gc
		gc.streams[s.key] = s
		delete(c.streams, s.key)
	}

	c.parent.mu.Lock()
	delete(c.parent.clients, c.id)
	gc.LockedPing()
	c.parent.mu.Unlock()
	c.mu.Unlock()
	gc.mu.Unlock()
}

func (c *gameClientWatch) Run(p StreamPacket) error {
	if c.Check(p) {
		slog.Debug("success matching unknown flow", "stream", p.stream.key.String())
		if err := p.stream.gameClient.Run(p); err != nil {
			return err
		}
		return nil
	}
	c.Check(p)
	if p.stream.sType == ST_UNKNOWN {
		p.stream.Identify(p.packet)
	}
	if c.Check(p) {
		slog.Debug("fallback matching unknown flow", "stream", p.stream.key.String())
		if err := p.stream.gameClient.Run(p); err != nil {
			return err
		}
	}
	switch p.stream.sType {
	case ST_WORLD, ST_LOGIN: // Wait for a zone event at minimum.
		c.newPredictClient(p)
		return p.stream.gameClient.Run(p)
	}
	return nil
}

func (c *gameClientWatch) getFirstClient() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k := range c.clients {
		return k.String()
	}
	return ""
}

// WaitForClient obtains a named client, or can block waiting for first avaliable.
func (c *gameClientWatch) WaitForClient(ctx context.Context, id string) (*gameClient, error) {
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

func (c *gameClient) PingChan() chan struct{} {
	c.mu.RLock()
	p := c.ping
	c.mu.RUnlock()
	return p
}
