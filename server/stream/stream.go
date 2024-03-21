package stream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gopacket/gopacket"
	"google.golang.org/grpc/peer"

	"github.com/nomoresecretz/ghoeq-common/decoder"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/common/ringbuffer"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"github.com/nomoresecretz/ghoeq/server/common"
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
	"OP_DataRate":      {},
}

type StreamType uint

//go:generate enumer -type=StreamType

const (
	ST_UNKNOWN StreamType = iota
	ST_LOGIN
	ST_WORLD
	ST_ZONE
	ST_CHAT
)

type Stream struct {
	ID         string
	Key        assembler.Key
	Net        gopacket.Flow
	Port       gopacket.Flow
	Dir        assembler.FlowDirection
	RB         *ringbuffer.RingBuffer[StreamPacket]
	Type       StreamType
	SF         *StreamFactory
	GameClient gameClient

	mu                sync.RWMutex
	seq               uint64
	Created, LastSeen time.Time
	Clients           map[uuid.UUID]*StreamClient
	LastClient        time.Time

	once sync.Once
	ch   chan<- StreamPacket
}

const (
	packetBufferSize = 20
)

func (s *Stream) Clean() {
	s.mu.Lock()
	if s.GameClient != nil {
		s.GameClient.DeleteStream(s.Key)
	}

	s.Close()
	s.mu.Unlock()
}

func (s *Stream) Proto() *pb.Stream {
	return &pb.Stream{
		Id:          s.Key.String(),
		Address:     s.Net.Src().String(),
		Port:        s.Port.Src().String(),
		PeerAddress: s.Net.Dst().String(),
		PeerPort:    s.Port.Dst().String(),
		Type:        pb.PeerType(s.Type),
		Direction:   pb.Direction(s.Dir),
	}
}

func (s *Stream) Close() {
	ch := s.ch
	s.ch = nil
	s.once.Do(func() { close(ch) }) // Lazy fix because I'm tired.
}

func (s *Stream) Accept(p gopacket.Layer, ci gopacket.CaptureInfo, dir assembler.FlowDirection, nSeq assembler.Sequence, start *bool, ac assembler.AssemblerContext) bool {
	return s.ch != nil
}

func (s *Stream) FanOut(ctx context.Context, p StreamPacket) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.ch <- p:
		if s.ch == nil {
			return fmt.Errorf("attempt to send on closed stream")
		}
	default:
		slog.Error("failed to send packet", "packet", p)
	}

	return nil
}

func (s *Stream) Process(ctx context.Context, p StreamPacket) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.SF.cout <- p:
	}

	return nil
}

func (s *Stream) String() string {
	return s.Proto().String()
}

func (s *Stream) Identify(p *eqOldPacket.EQApplication) {
	sf := s.SF

	switch decoder.OpCode(p.OpCode) {
	case sf.oc["OP_LoginPC"]:
		s.Dir = assembler.DirClientToServer
		s.Type = ST_LOGIN
	case sf.oc["OP_DataRate"]:
		s.Dir = assembler.DirClientToServer
		s.Type = ST_ZONE
	case sf.oc["OP_SessionReady"]:
		s.Dir = assembler.DirServerToClient
		s.Type = ST_LOGIN
	case sf.oc["OP_LoginAccepted"]:
		s.Dir = assembler.DirServerToClient
		s.Type = ST_LOGIN
	case sf.oc["OP_SendLoginInfo"]:
		s.Dir = assembler.DirClientToServer
		s.Type = ST_WORLD
	case sf.oc["OP_LogServer"]:
		s.Dir = assembler.DirServerToClient
		s.Type = ST_WORLD
	case sf.oc["OP_ZoneEntry"]:
		s.Type = ST_ZONE
	case sf.oc["OP_PlayerProfile"]:
		s.Dir = assembler.DirServerToClient
		s.Type = ST_ZONE
	}

	if s.Type != ST_UNKNOWN {
		slog.Info("identified stream type", "stream", s.Key.String(), "type", s.Type.String(), "direction", s.Dir.String(), "packet", p.OpCode)
	}

	// still unknown, lets check for our mate.
	if s.Type == ST_UNKNOWN || s.Dir == assembler.DirUnknown {
		rev := s.Key.Reverse()
		mgr := s.SF.Mgr()
		mgr.Mu().RLock()

		// Just mirror our mate if it exists.
		rstrm, ok := mgr.ClientStreams()[rev]
		if ok && rstrm.Type != ST_UNKNOWN {
			s.Dir = rstrm.Dir.Reverse()
			s.Type = rstrm.Type
			slog.Debug("identified stream by mate", "stream", s.Key.String())
		}
		mgr.Mu().RUnlock()
	}
}

func (s *Stream) AttachToStream(ctx context.Context) (*StreamClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan StreamPacket, common.ClientBuffer)
	cinfo, ok := peer.FromContext(ctx)

	var clientTag string
	if ok {
		clientTag = cinfo.Addr.String()
	}

	slog.Info("capture stream adding client", "type", s.Type.String(), "stream", s.Key.String(), "client", clientTag)

	id := uuid.New()
	c := &StreamClient{
		Handle: ch,
		info:   clientTag,
		Parent: s,
		ID:     id,
	}
	s.Clients[id] = c

	return c, nil
}

// handleClients runs the fanout loop for client sessions.
func (s *Stream) handleClients(ctx context.Context, apcb <-chan StreamPacket) error {
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

func (s *Stream) DeleteClient(id uuid.UUID) {
	s.mu.Lock()
	delete(s.Clients, id)
	s.mu.Unlock()
}

func (s *Stream) handleClientSend(ctx context.Context, ap StreamPacket) {
	s.mu.RLock()
	for _, c := range s.Clients {
		c.Send(ctx, ap)
	}
	s.mu.RUnlock()
}

func (s *Stream) handleClientClose() error {
	s.mu.RLock()
	for _, sc := range s.Clients {
		if sc.Handle == nil {
			continue
		}

		handle := sc.Handle
		sc.Handle = nil

		close(handle)
	}
	s.mu.RUnlock()

	return nil // chan closure
}
