package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/nomoresecretz/ghoeq-common/decoder"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"github.com/nomoresecretz/ghoeq/server/common"
	"github.com/nomoresecretz/ghoeq/server/game_client"
	"github.com/nomoresecretz/ghoeq/server/stream"
	"golang.org/x/sync/errgroup"
)

type streamMgr struct {
	packets   uint64
	eqpackets uint64
	decoder   common.OpDecoder

	mu            *sync.RWMutex
	clientStreams map[assembler.Key]*stream.Stream
	streamMap     map[string]assembler.Key
	clientWatch   *game_client.GameClientWatch
	session       *session
}

func NewStreamMgr(d common.OpDecoder, cw *game_client.GameClientWatch) *streamMgr {
	cw.Decoder = d // this is a terrible hack.
	return &streamMgr{
		clientStreams: make(map[assembler.Key]*stream.Stream),
		streamMap:     make(map[string]assembler.Key),
		decoder:       d,
		clientWatch:   cw,
	}
}

type pcapHandle interface {
	SetBPFFilter(string) error
	LinkType() layers.LinkType
	gopacket.PacketDataSource
	Close()
}

// NewCapture sets up a new capture session on an interface / source.
func (sm *streamMgr) NewCapture(ctx context.Context, h pcapHandle, cout chan<- stream.StreamPacket, wg *errgroup.Group) error {
	if err := h.SetBPFFilter("port 9000 or port 6000 or portrange 7000-7400"); err != nil {
		return (err)
	}

	streamFactory := stream.NewStreamFactory(sm, cout, wg)
	streamPool := assembler.NewStreamPool(streamFactory)
	streamAsm := assembler.NewAssembler(streamPool)

	defer streamPool.Close()

	c := NewCapture(h)
	pChan := c.Packets(ctx)

	defer close(cout)

	var done bool
	for !done {
		select {
		case <-ctx.Done():
			done = true
		case p, ok := <-pChan:
			if !ok {
				done = true

				break
			}

			if err := sm.handlePacket(ctx, p, streamAsm); err != nil {
				return err
			}
		}
	}

	return nil
}

type asm interface {
	Assemble(ctx context.Context, net, port gopacket.Flow, op *eqOldPacket.OldEQOuter) (*eqOldPacket.OldEQOuter, assembler.Stream, error)
}

func (sm *streamMgr) Mu() *sync.RWMutex {
	return sm.mu
}

func (sm *streamMgr) ClientStreams() map[assembler.Key]*stream.Stream {
	return sm.clientStreams
}

func (sm *streamMgr) AddStream(key assembler.Key, s *stream.Stream) {
	sm.mu.Lock()
	sm.clientStreams[key] = s
	sm.streamMap[s.ID] = key
	sm.mu.Unlock()
}

func (sm *streamMgr) Decoder() common.OpDecoder {
	return sm.decoder
}

func (sm *streamMgr) handlePacket(ctx context.Context, p gopacket.Packet, streamAsm asm) error {
	sm.packets++

	// Skip non interesting packets
	o := p.Layer(eqOldPacket.OldEQOuterType)
	if o == nil {
		return nil
	}

	sm.eqpackets++
	// Reassemble fragment packets
	op, _ := o.(*eqOldPacket.OldEQOuter)

	op, pStream, err := streamAsm.Assemble(ctx, p.NetworkLayer().NetworkFlow(), p.TransportLayer().TransportFlow(), op)
	if err != nil {
		return err
	}

	if op == nil {
		return nil
	}

	if len(op.Payload) == 0 {
		return nil
	}

	np := gopacket.NewPacket(op.Payload, eqOldPacket.EQApplicationType, gopacket.Default)

	eqold := np.Layer(eqOldPacket.EQApplicationType)
	if eqold == nil {
		return nil
	}

	ap, ok := eqold.(*eqOldPacket.EQApplication)
	if !ok {
		return fmt.Errorf("improper packet type %t", eqold)
	}

	sp := stream.StreamPacket{
		Origin: p.Metadata().Timestamp,
		Seq:    uint64(op.Seq),
		Stream: pStream.(*stream.Stream),
		Packet: ap,
		OpCode: decoder.OpCode(ap.OpCode),
	}

	if err := pStream.(*stream.Stream).Process(ctx, sp); err != nil {
		return err
	}

	return nil
}

func (sm *streamMgr) Close() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.clientStreams {
		s.Close()
	}
	sm.session.Close()
}

func (sm *streamMgr) StreamById(streamId string) (*stream.Stream, error) {
	sm.mu.RLock()

	k, ok := sm.streamMap[streamId]
	if !ok {
		return nil, fmt.Errorf("unknown stream id: %s", streamId)
	}

	str, ok := sm.clientStreams[k]
	if !ok {
		return nil, fmt.Errorf("missing stream: %s", k.String())
	}
	sm.mu.RUnlock()

	return str, nil
}
