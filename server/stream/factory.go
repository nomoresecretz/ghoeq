package stream

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/gopacket/gopacket"
	"github.com/nomoresecretz/ghoeq-common/decoder"
	"github.com/nomoresecretz/ghoeq/common/ringbuffer"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"golang.org/x/sync/errgroup"
)

const defaultRingSize = 100 // packets

type StreamFactory struct {
	mgr  streamMgr
	cout chan<- StreamPacket
	oc   map[string]decoder.OpCode
	wg   *errgroup.Group
}

func (sf *StreamFactory) Mgr() streamMgr {
	return sf.mgr
}

func (sf *StreamFactory) New(ctx context.Context, netFlow, portFlow gopacket.Flow, l gopacket.Layer, ac assembler.AssemblerContext) assembler.Stream {
	key := assembler.Key{netFlow, portFlow}
	ch := make(chan StreamPacket, packetBufferSize)
	s := &Stream{
		ID:       key.String(),
		Key:      key,
		Net:      netFlow,
		Port:     portFlow,
		RB:       ringbuffer.New[StreamPacket](defaultRingSize),
		SF:       sf,
		Clients:  make(map[uuid.UUID]*StreamClient),
		ch:       ch,
		Created:  now(),
		LastSeen: now(),
	}

	sf.Mgr().AddStream(key, s)
	slog.Debug("tracking new stream", "stream", s.Proto())
	sf.wg.Go(func() error {
		return s.handleClients(ctx, ch)
	})

	return s
}

func NewStreamFactory(sm streamMgr, cout chan<- StreamPacket, wg *errgroup.Group) *StreamFactory {
	oc := make(map[string]decoder.OpCode)
	for k := range ocList {
		oc[k] = sm.Decoder().GetOpByName(k)
	}

	return &StreamFactory{
		cout: cout,
		mgr:  sm,
		oc:   oc,
		wg:   wg,
	}
}

func (sf *StreamFactory) Close() {
	sf.Mgr().Close()
}
