package main

import (
	"context"
	"sync"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
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
	streamMap map[string]assembler.Key
	clientWatch *gameClientWatch
}

type opDecoder interface {
	GetOp(uint16) string
	GetOpByName(string) uint16
}

func NewStreamMgr(d opDecoder, cw *gameClientWatch) *streamMgr {
	return &streamMgr{
		clientStreams: make(map[assembler.Key]*stream),
		streamMap: make(map[string]assembler.Key),
		decoder:       d,
		clientWatch: cw,
	}
}

// NewCapture sets up a new capture session on an interface / source.
func (sm *streamMgr) NewCapture(ctx context.Context, h *pcap.Handle, cout chan<- streamPacket, wg *errgroup.Group) error {
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

			sm.packets++

			// Skip non interesting packets
			o := p.Layer(eqOldPacket.OldEQOuterType)
			if o == nil {
				continue
			}

			sm.eqpackets++
			// Reassemble fragment packets
			op, _ := o.(*eqOldPacket.OldEQOuter)

			op, stream, err := streamAsm.Assemble(ctx, p.NetworkLayer().NetworkFlow(), p.TransportLayer().TransportFlow(), op)
			if err != nil {
				return err
			}

			if op == nil {
				continue
			}

			var payload []byte
			copy(payload, op.Payload) // Checking if this fixes a repeat bug.
			p = gopacket.NewPacket(payload, eqOldPacket.EQApplicationType, gopacket.Default)

			eqold := p.Layer(eqOldPacket.EQApplicationType)
			if eqold == nil {
				continue
			}

			if err := stream.Send(ctx, eqold, op.Seq); err != nil {
				return err
			}
		}
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