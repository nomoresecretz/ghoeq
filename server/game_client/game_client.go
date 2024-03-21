package game_client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"github.com/nomoresecretz/ghoeq/server/common"
	"github.com/nomoresecretz/ghoeq/server/stream"
)

const (
	lenRaidCreate    = 72
	lenRaidAddMember = 144
	lenRaidGeneral   = 136
	lenLFGAppearance = 8
	lenLFGCommand    = 68
)

const (
	maxPredictTime   = 60 * time.Second
	defaultFudgeTime = 1 * time.Second
)

var letterTest = regexp.MustCompile(`[a-zA-Z]`)

type predictEntry struct {
	created time.Time
	client  *GameClient
	sType   stream.StreamType
	dir     assembler.FlowDirection
}

type streamPredict map[string]predictEntry

type GameClient struct {
	ID              uuid.UUID
	Mu              *sync.RWMutex
	Streams         map[assembler.Key]*stream.Stream
	clients         map[uuid.UUID]*stream.StreamClient
	Ping            chan struct{}
	decoder         common.OpDecoder
	parent          *GameClientWatch
	clientServer    string        // game server name.
	clientSessionID string        // Assigned by server.
	clientCharacter string        // game client character.
	zoneStream      assembler.Key // server stream of current zone.
	PlayerProfile   *eqStruct.PlayerProfile
	db              *DB
	ch              chan stream.StreamPacket
}

func (gc *GameClient) DeleteClient(id uuid.UUID) {
	gc.Mu.Lock()
	delete(gc.clients, id)
	gc.Mu.Unlock()
}

func NewPredict(gc *GameClient, s stream.StreamType, dir assembler.FlowDirection) predictEntry {
	return predictEntry{
		created: time.Now(),
		client:  gc,
		sType:   s,
		dir:     dir,
	}
}

func (c *GameClient) DeleteStream(s assembler.Key) {
	c.Mu.Lock()
	delete(c.Streams, s)
	c.Mu.Unlock()
}

func (c *GameClient) AddStream(s *stream.Stream) {
	slog.Debug("gameClient stream added")
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Streams[s.Key] = s
	c.LockedPing()
}

func (c *GameClient) LockedPing() {
	ping := c.Ping
	c.Ping = make(chan struct{})

	close(ping)
}

func (c *GameClient) Close() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.ClosePing()
	c.parent.mu.Lock()
	delete(c.parent.charMap, c.clientCharacter)
	c.parent.mu.Unlock()
	close(c.ch)
}

func (c *GameClient) ClosePing() {
	ping := c.Ping
	c.Ping = nil

	if ping != nil {
		close(ping)
	}
}

func New(ctx context.Context, c *GameClientWatch) *GameClient {
	gc := &GameClient{
		ID:      uuid.New(),
		Streams: make(map[assembler.Key]*stream.Stream),
		Ping:    make(chan struct{}),
		decoder: c.Decoder,
		parent:  c,
		db:      c.db,
		ch:      make(chan stream.StreamPacket, common.ClientBuffer),
		clients: make(map[uuid.UUID]*stream.StreamClient),
		Mu:      &sync.RWMutex{},
	}

	go gc.fanoutRunner(ctx) // TODO: track this goroutine properly.

	return gc
}

func (gc *GameClient) fanoutRunner(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			close(gc.ch)
		case p, ok := <-gc.ch:
			if !ok {
				gc.Mu.Lock()
				defer gc.Mu.Unlock()
				gc.closeClientsLocked()

				return
			}

			gc.Mu.RLock()

			for _, c := range gc.clients {
				c.Send(ctx, p)
			}

			gc.Mu.RUnlock()
		}
	}
}

func (gc *GameClient) closeClientsLocked() {
	for _, c := range gc.clients {
		c.Close()
	}
}

func (c *GameClient) Run(ctx context.Context, p *stream.StreamPacket) error {
	if err := c.process(p); err != nil {
		return err
	}

	if err := c.db.Update(p); err != nil {
		return err
	}

	return c.clientSend(ctx, p)
}

func (gc *GameClient) clientSend(ctx context.Context, p *stream.StreamPacket) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case gc.ch <- *p:
		if gc.ch == nil {
			return fmt.Errorf("attempt to send on closed stream")
		}
	default:
		slog.Error("failed to send packet", "packet", p)
	}

	return nil
}

func (c *GameClient) Historic(ctx context.Context, sf func(s stream.StreamPacket) error, epoch time.Time) error {
	rt := epoch.Add(defaultFudgeTime) // fudge back a bit.

	strm := c.Streams[c.zoneStream]
	if strm == nil {
		return nil
	}

	sp, err := c.db.HistoricSpawns(strm.ID, rt)
	if err != nil {
		return err
	}

	for _, v := range sp {
		if err := sf(stream.StreamPacket{
			Origin: v.SpawnTime,
			Obj:    v,
			Stream: strm,
		}); err != nil {
			return err
		}
	}

	upds, err := c.db.HistoricSpawnUpdates(strm.ID, rt)
	if err != nil {
		return err
	}

	for _, v := range upds {
		if err := sf(stream.StreamPacket{
			Origin: v.LastUpdate,
			Obj:    v,
			Stream: strm,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (c *GameClient) AttachToStream(ctx context.Context) (*stream.StreamClient, error) {
	c.Mu.Lock()
	defer c.Mu.Unlock()

	ch := make(chan stream.StreamPacket, common.ClientBuffer)

	slog.Info("game client adding stream client", "client", c.ID)

	id := uuid.New()
	sc := &stream.StreamClient{
		Handle: ch,
		Parent: c,
		ID:     id,
	}

	c.clients[id] = sc

	return sc, nil
}

func (c *GameClient) matchChar(n string) bool {
	gc := c.parent.matchChar(n)
	if gc == nil || gc == c {
		return false
	}

	slog.Info("matched extra client by charName, reducing", "name", n)

	c.reduce(gc)

	return true
}

func (c *GameClient) matchAcct(n string) bool {
	gc := c.parent.matchAcct(n)
	if gc == nil || gc == c {
		return false
	}

	slog.Info("matched extra client by account, reducing", "name", n)

	c.reduce(gc)

	return true
}

func (c *GameClient) reduce(gc *GameClient) {
	gc.Mu.Lock()
	c.Mu.Lock()

	for _, s := range c.Streams {
		s.GameClient = gc
		gc.Streams[s.Key] = s
		delete(c.Streams, s.Key)
	}

	c.parent.mu.Lock()
	delete(c.parent.clients, c.ID)
	gc.LockedPing()
	c.parent.mu.Unlock()
	c.Mu.Unlock()
	gc.Mu.Unlock()
}

func (c *GameClient) PingChan() chan struct{} {
	c.Mu.RLock()
	p := c.Ping
	c.Mu.RUnlock()

	return p
}

func handleOpCode[T eqStruct.EQStruct](s T, p *stream.StreamPacket) error {
	if _, err := s.Unmarshal(p.Packet.Payload); err != nil {
		slog.Error("failed unmarshaling", "error", err, "struct", s)

		return fmt.Errorf("failed to unmarshal %T: %w", s, err)
	}

	p.Obj = s

	return nil
}

func dnsCheck(field *string) error {
	dns := letterTest.FindString(*field)
	if dns != "" {
		ip, err := net.LookupIP(*field)
		if err != nil {
			return fmt.Errorf("failed dns lookup: %w", err)
		}

		*field = ip[0].String()
	}

	return nil
}
