package main

import (
	"context"
	"sync"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

type streamMgr struct {
	packets   uint64
	eqpackets uint64
	decoder   opDecoder

	mu            sync.RWMutex
	clientStreams map[assembler.Key]any
}

type opDecoder interface{
	GetOp(uint16) string
	GetOpByName(string) uint16
}

func NewStreamMgr(d opDecoder) *streamMgr {
	return &streamMgr{
		clientStreams: make(map[assembler.Key]any),
		decoder:       d,
	}
}

// NewCapture sets up a new capture session on an interface / source.
func (sm *streamMgr) NewCapture(ctx context.Context, h *pcap.Handle, cout chan<- streamPacket) error {
	if err := h.SetBPFFilter("port 9000 or port 6000 or portrange 7000-7400"); err != nil {
		return (err)
	}

	streamFactory := NewStreamFactory(sm, cout)
	streamPool := assembler.NewStreamPool(streamFactory)
	streamAsm := assembler.NewAssembler(streamPool)

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

			payload := op.Payload
			p = gopacket.NewPacket(payload, eqOldPacket.EQApplicationType, gopacket.Default)

			eqold := p.Layer(eqOldPacket.EQApplicationType)
			if eqold == nil {
				continue
			}

			if err := stream.Send(ctx, eqold); err != nil {
				return err
			}
		}
	}

	return nil
}
