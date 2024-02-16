package main

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
)

type streamMgr struct {
	packets   uint64
	eqpackets uint64
}

func NewStreamMgr() *streamMgr {
	return &streamMgr{}
}

type (
	assembler struct {
		p *streamPool
	}
	streamPool struct {
		mu    sync.RWMutex
		frags map[uint16]*fragI
	}
	streamFactory struct{}
)

type fragment struct {
	Payload []byte
}

type fragI struct {
	origin time.Time
	seen   uint16
	frags  []fragment
}

// TODO: Implement proper unique  stream tracking.

//NewAssembler returns a stream aware packet fragment assembler.
func NewAssembler(s *streamPool) *assembler {
	return &assembler{
		p: s,
	}
}

//NewStreamPool is intended to be a pool for tracking the various known streams, this is just a blind shell to be implemented later.
func NewStreamPool(f *streamFactory) *streamPool {
	return &streamPool{
		frags: make(map[uint16]*fragI),
	}
}

//Assemble is a reassembler for fragmented Old style packets.
func (a *assembler) Assemble(np *OldEQOuter) (*OldEQOuter, error) {
	pool := a.p
	pool.mu.Lock()
	defer pool.mu.Unlock()

	id := np.fh.dwFragSEQ
	me := np.fh.dwCurr
	ttl := np.fh.dwTotal
	if _, ok := pool.frags[id]; !ok {
		pool.frags[id] = &fragI{
			origin: time.Now(),
			frags:  make([]fragment, ttl),
			seen:   0,
		}
	}
	fi := pool.frags[id]
	if fi.frags[me].Payload == nil {
		fi.frags[me] = fragment{Payload: np.Payload}
		fi.seen++
	}
	if fi.seen == uint16(len(fi.frags)) {
		var b bytes.Buffer
		for _, v := range fi.frags {
			b.Write(v.Payload)
		}
		np.Payload = b.Bytes()
		delete(pool.frags, id)
		return np, nil
	}
	return nil, nil
}

// NewCapture sets up a new capture session on an interface / source.
func (s *streamMgr) NewCapture(ctx context.Context, h *pcap.Handle, cout chan<- *EQApplication) error {
	if err := h.SetBPFFilter("port 9000 or port 6000 or portrange 7000-7400"); err != nil {
		return (err)
	}
	streamFactory := &streamFactory{}
	streamPool := NewStreamPool(streamFactory)
	fragAsm := NewAssembler(streamPool)

	c := NewCapture(h)

	for p := range c.Packets() {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.packets++
		// Skip non interesting packets
		o := p.Layer(OldEQOuterType)
		if o == nil {
			continue
		}
		s.eqpackets++
		// Reassemble fragment packets
		op := o.(*OldEQOuter)
		if op.hasFlag(Frag) {
			op, err := fragAsm.Assemble(op)
			if err != nil {
				return err
			}
			if op == nil {
				continue
			}
		}
		payload := op.Payload
		p := gopacket.NewPacket(payload, EQApplicationType, gopacket.Default)
		eqold := p.Layer(EQApplicationType)
		if eqold == nil {
			continue
		}
		ep, _ := eqold.(*EQApplication)
		select {
		case <-ctx.Done():
		case cout <- ep:
		}
	}
	close(cout)
	return nil
}
