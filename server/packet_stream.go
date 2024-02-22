package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
	"github.com/nomoresecretz/ghoeq/common/decoder"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/server/assembler"
	"golang.org/x/sync/errgroup"
)

type streamMgr struct {
	packets   uint64
	eqpackets uint64
	decoder   opDecoder

	mu            sync.RWMutex
	clientStreams map[assembler.Key]*stream
	streamMap     map[string]assembler.Key
	clientWatch   *gameClientWatch
	session       *session
}

type opDecoder interface {
	GetOp(decoder.OpCode) string
	GetOpByName(string) decoder.OpCode
}

func NewStreamMgr(d opDecoder, cw *gameClientWatch) *streamMgr {
	cw.decoder = d // this is a terrible hack.
	return &streamMgr{
		clientStreams: make(map[assembler.Key]*stream),
		streamMap:     make(map[string]assembler.Key),
		decoder:       d,
		clientWatch:   cw,
	}
}

// NewCapture sets up a new capture session on an interface / source.
func (sm *streamMgr) NewCapture(ctx context.Context, h *pcap.Handle, cout chan<- StreamPacket, wg *errgroup.Group) error {
	if err := h.SetBPFFilter("port 9000 or port 6000 or portrange 7000-7400"); err != nil {
		return (err)
	}

	streamFactory := NewStreamFactory(sm, cout, wg)
	streamPool := assembler.NewStreamPool(streamFactory)
	streamAsm := assembler.NewAssembler(streamPool)
	defer streamFactory.Close()

	c := NewCapture(h)
	pChan := c.Packets()

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

	p = gopacket.NewPacket(op.Payload, eqOldPacket.EQApplicationType, gopacket.Default)

	eqold := p.Layer(eqOldPacket.EQApplicationType)
	if eqold == nil {
		return nil
	}

	ap, ok := eqold.(*eqOldPacket.EQApplication)
	if !ok {
		return fmt.Errorf("improper packet type %t", eqold)
	}

	sp := StreamPacket{
		seq:    uint64(op.Seq),
		stream: pStream.(*stream),
		packet: ap,
		opCode: decoder.OpCode(ap.OpCode),
	}

	if err := pStream.(*stream).Process(ctx, sp); err != nil {
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
}

func (sm *streamMgr) StreamById(streamId string) (*stream, error) {
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