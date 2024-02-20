package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gopacket/gopacket"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/peer"

	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/common/ringbuffer"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

var now = func() time.Time {
	return time.Now()
}

var ocList = map[string]struct{}{
	"OP_LoginPC":       {},
	"OP_LoginAccepted": {},
	"OP_SendLoginInfo": {},
	"OP_LogServer":     {},
	"OP_ZoneEntry":     {},
	"OP_PlayerProfile": {},
}

type streamFactory struct {
	mgr  *streamMgr
	cout chan<- streamPacket
	oc   map[string]uint16
	wg   *errgroup.Group
}

type streamType uint

func (s streamType) String() string {
	switch s {
	case ST_LOGIN:
		return "ST_LOGIN"
	case ST_WORLD:
		return "ST_WORLD"
	case ST_ZONE:
		return "ST_ZONE"
	default:
		return "ST_UNKNOWN"
	}
}

const (
	ST_UNKNOWN streamType = iota
	ST_LOGIN              = 1
	ST_WORLD              = 2
	ST_ZONE               = 3
	ST_CHAT               = 4
)

type stream struct {
	key   assembler.Key
	net   gopacket.Flow
	port  gopacket.Flow
	dir   assembler.FlowDirection
	rb    *ringbuffer.RingBuffer[*eqOldPacket.EQApplication]
	sType streamType
	sf    *streamFactory
	ch    chan<- streamPacket

	mu                sync.RWMutex
	seq               uint64
	created, lastSeen time.Time
	clients           map[uuid.UUID]*streamClient
	lastClient        time.Time
}

const (
	packetBufferSize = 20
)

type streamPacket struct {
	stream *stream
	packet *eqOldPacket.EQApplication
	seq    uint64
}

func (sf *streamFactory) New(ctx context.Context, netFlow, portFlow gopacket.Flow, l gopacket.Layer, ac assembler.AssemblerContext) assembler.Stream {
	key := assembler.Key{netFlow, portFlow}
	ch := make(chan streamPacket)
	s := &stream{
		key:      key,
		net:      netFlow,
		port:     portFlow,
		rb:       ringbuffer.New[*eqOldPacket.EQApplication](100),
		sf:       sf,
		clients:  make(map[uuid.UUID]*streamClient),
		ch:       ch,
		created:  now(),
		lastSeen: now(),
	}
	sf.mgr.mu.Lock()
	sf.mgr.clientStreams[key] = s
	sf.mgr.streamMap[key.String()] = key
	sf.mgr.mu.Unlock()
	sf.wg.Go(func() error {
		return s.handleClients(ctx, ch)
	})
	return s
}

func (s *stream) Accept(p gopacket.Layer, ci gopacket.CaptureInfo, dir assembler.FlowDirection, nSeq assembler.Sequence, start *bool, ac assembler.AssemblerContext) bool {
	return true
}

func NewStreamFactory(sm *streamMgr, cout chan<- streamPacket, wg *errgroup.Group) *streamFactory {
	oc := make(map[string]uint16)
	for k := range ocList {
		oc[k] = sm.decoder.GetOpByName(k)
	}

	return &streamFactory{
		cout: cout,
		mgr:  sm,
		oc:   oc,
		wg:   wg,
	}
}

func (s *stream) Clean() {
	// TODO: clean up dead stream.
	panic("unimplemented")
}

func (s *stream) Send(ctx context.Context, l gopacket.Layer, seq uint16) error {
	p, ok := l.(*eqOldPacket.EQApplication)
	if !ok {
		return fmt.Errorf("improper packet type %t", l)
	}

	return s.HandleAppPacket(ctx, p, seq)
}

func (s *stream) Close() {
	close(s.ch)
}

func (s *stream) FanOut(ctx context.Context, p streamPacket) error {
	select {
	case <-ctx.Done():
	case s.ch <- p:
	default:
		slog.Error("failed to send packet", "packet", p)
	}
	return nil
}

func (s *stream) HandleAppPacket(ctx context.Context, p *eqOldPacket.EQApplication, seq uint16) error {
	s.rb.Add(p) // TODO: store the sequence numbers somehow.

	select {
	case <-ctx.Done():
	case s.sf.cout <- streamPacket{
		seq:    uint64(seq),
		stream: s,
		packet: p,
	}:
	default:
		slog.Error("failed to send packet to fanout")
	}

	return nil
}

func (s *stream) Identify(ctx context.Context, p *eqOldPacket.EQApplication) {
	switch p.OpCode {
	case s.sf.oc["OP_LoginPC"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_LOGIN
	case s.sf.oc["OP_LoginAccepted"]:
		s.dir = assembler.DirServerToClient
		s.sType = ST_LOGIN
	case s.sf.oc["OP_SendLoginInfo"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_WORLD
	case s.sf.oc["OP_LogServer"]:
		s.dir = assembler.DirServerToClient
		s.sType = ST_WORLD
	case s.sf.oc["OP_ZoneEntry"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_ZONE
	case s.sf.oc["OP_PlayerProfile"]:
		s.dir = assembler.DirServerToClient
		s.sType = ST_ZONE
	}

	// still unknown, lets check for our mate.
	if s.sType == ST_UNKNOWN {
		rev := s.key.Reverse()
		s.sf.mgr.mu.RLock()
	
		// Just mirror our mate if it exists.
		rstrm, ok := s.sf.mgr.clientStreams[rev]
		if ok && rstrm.sType != ST_UNKNOWN {
			s.dir = rstrm.dir.Reverse()
			s.sType = rstrm.sType
		}
		s.sf.mgr.mu.RUnlock()
	
	}

	// TODO: Actual predictive client tracking based on packet contents.
}

func (s *stream) AttachToStream(ctx context.Context) (*streamClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan streamPacket, clientBuffer)
	cinfo, ok := peer.FromContext(ctx)

	var clientTag string
	if ok {
		clientTag = cinfo.Addr.String()
	}

	slog.Info("capture stream adding client", "stream", s.key.String(), "client", clientTag)

	id := uuid.New()
	c := &streamClient{
		handle: ch,
		info:   clientTag,
		parent: s,
		id:     id,
	}
	s.clients[id] = c

	return c, nil
}

type streamClient struct {
	id     uuid.UUID
	handle chan streamPacket
	info   string
	parent *stream
}

// Send relays the packet to the attached client session.
func (c *streamClient) Send(ctx context.Context, ap streamPacket) {
	// TODO: convert this to a ring buffer
	select {
	case c.handle <- ap:
	case <-ctx.Done():
	default:
		slog.Error("failed to send to client", "client", c, "opcode", ap.packet.OpCode)
	}
}

func (c *streamClient) String() string {
	return c.info
}

func (sc *streamClient) Close() {
	slog.Info("client disconnecting", "client", sc.info)
	sc.parent.mu.Lock()
	delete(sc.parent.clients, sc.id)
	sc.parent.mu.Unlock()

	if sc.handle == nil {
		return
	}

	close(sc.handle)
	sc.handle = nil
}

// handleClients runs the fanout loop for client sessions.
func (s *stream) handleClients(ctx context.Context, apcb <-chan streamPacket) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ap, ok := <-apcb:
			if !ok {
				s.mu.RLock()
				for _, sc := range s.clients {
					if sc.handle == nil {
						continue
					}

					close(sc.handle)
					sc.handle = nil
				}
				s.mu.RUnlock()

				return nil // chan closure
			}

			s.mu.RLock()
			for _, c := range s.clients {
				c.Send(ctx, ap)
			}
			s.mu.RUnlock()
		}
	}
}
