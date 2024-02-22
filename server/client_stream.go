package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gopacket/gopacket"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/peer"

	"github.com/nomoresecretz/ghoeq/common/decoder"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	pb "github.com/nomoresecretz/ghoeq/common/proto/ghoeq"
	"github.com/nomoresecretz/ghoeq/common/ringbuffer"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

var now = func() time.Time {
	return time.Now()
}

var ocList = map[string]struct{}{
	"OP_LoginPC":       {},
	"OP_SessionReady":  {},
	"OP_LoginAccepted": {},
	"OP_SendLoginInfo": {},
	"OP_LogServer":     {},
	"OP_ZoneEntry":     {},
	"OP_PlayerProfile": {},
}

type streamFactory struct {
	mgr  *streamMgr
	cout chan<- StreamPacket
	oc   map[string]decoder.OpCode
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
	id         string
	key        assembler.Key
	net        gopacket.Flow
	port       gopacket.Flow
	dir        assembler.FlowDirection
	rb         *ringbuffer.RingBuffer[StreamPacket]
	sType      streamType
	sf         *streamFactory
	gameClient *gameClient

	mu                sync.RWMutex
	seq               uint64
	created, lastSeen time.Time
	clients           map[uuid.UUID]*streamClient
	lastClient        time.Time

	once sync.Once
	ch   chan<- StreamPacket
}

const (
	packetBufferSize = 20
)

type StreamPacket struct {
	stream *stream
	packet *eqOldPacket.EQApplication
	seq    uint64
	opCode decoder.OpCode
}

func (s *StreamPacket) Proto() *pb.APPacket {
	return &pb.APPacket{
		Seq: s.seq,
		OpCode: uint32(s.opCode),
		Data: s.packet.Payload,
	}
}

func (sf *streamFactory) New(ctx context.Context, netFlow, portFlow gopacket.Flow, l gopacket.Layer, ac assembler.AssemblerContext) assembler.Stream {
	key := assembler.Key{netFlow, portFlow}
	ch := make(chan StreamPacket, packetBufferSize)
	s := &stream{
		key:      key,
		net:      netFlow,
		port:     portFlow,
		rb:       ringbuffer.New[StreamPacket](100),
		sf:       sf,
		clients:  make(map[uuid.UUID]*streamClient),
		ch:       ch,
		created:  now(),
		lastSeen: now(),
	}

	sf.mgr.mu.Lock()
	sf.mgr.clientStreams[key] = s
	s.id = key.String()
	sf.mgr.streamMap[s.id] = key
	sf.mgr.mu.Unlock()
	slog.Debug("tracking new stream", "stream", s.Proto())
	sf.wg.Go(func() error {
		return s.handleClients(ctx, ch)
	})

	return s
}

func NewStreamFactory(sm *streamMgr, cout chan<- StreamPacket, wg *errgroup.Group) *streamFactory {
	oc := make(map[string]decoder.OpCode)
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

func (sf *streamFactory) Close() {
	sf.mgr.Close()
}

func (s *stream) Clean() {
	// TODO: clean up dead stream.
	panic("unimplemented")
}

func (s *stream) Proto() *pb.Stream {
	return &pb.Stream{
		Id:          s.key.String(),
		Address:     s.net.Src().String(),
		Port:        s.port.String(),
		PeerAddress: s.net.Dst().String(),
		PeerPort:    s.port.Dst().String(),
		Type:        pb.PeerType(s.sType),
		Direction:   pb.Direction(s.dir),
	}
}

func (s *stream) Close() {
	s.once.Do(func() { close(s.ch) }) // Lazy fix because I'm tired.
}

func (s *stream) Accept(p gopacket.Layer, ci gopacket.CaptureInfo, dir assembler.FlowDirection, nSeq assembler.Sequence, start *bool, ac assembler.AssemblerContext) bool {
	return true
}

func (s *stream) FanOut(ctx context.Context, p StreamPacket) error {
	select {
	case <-ctx.Done():
	case s.ch <- p:
	default:
		slog.Error("failed to send packet", "packet", p)
	}

	return nil
}

func (s *stream) Process(ctx context.Context, p StreamPacket) error {
	s.rb.Add(p) // TODO: store the sequence numbers somehow.

	select {
	case <-ctx.Done():
	case s.sf.cout <- p:
	default:
		slog.Error("failed to send packet to processing")
	}

	return nil
}

func (s *stream) String() string {
	return s.Proto().String()
}

func (s *stream) Identify(p *eqOldPacket.EQApplication) {
	switch decoder.OpCode(p.OpCode) {
	case s.sf.oc["OP_LoginPC"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_LOGIN
	case s.sf.oc["OP_SessionReady"]:
		s.dir = assembler.DirServerToClient
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

	if s.sType != ST_UNKNOWN {
		slog.Debug("identified stream type", "stream", s.key.String(), "type", s.sType.String(), "direction", s.dir.String())
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
			slog.Debug("identified stream by mate", "stream", s.key.String())
		}
		s.sf.mgr.mu.RUnlock()
	}
}

func (s *stream) AttachToStream(ctx context.Context) (*streamClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan StreamPacket, clientBuffer)
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
	handle chan StreamPacket
	info   string
	parent *stream
}

// Send relays the packet to the attached client session.
func (c *streamClient) Send(ctx context.Context, ap StreamPacket) {
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
func (s *stream) handleClients(ctx context.Context, apcb <-chan StreamPacket) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ap, ok := <-apcb:
			if !ok {
				return s.handleClientClose()
			}

			s.handleClientSend(ctx, ap)
		}
	}
}

func (s *stream) handleClientSend(ctx context.Context, ap StreamPacket) {
	s.mu.RLock()
	for _, c := range s.clients {
		c.Send(ctx, ap)
	}
	s.mu.RUnlock()
}

func (s *stream) handleClientClose() error {
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
