package assembler

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
)

const (
	invalidSequence = -1
	uint32Max       = 0xFFFFFFFF
)

type (
	assembler struct {
		pool  *streamPool
		start bool
		watch uint16
	}
	Key           [2]gopacket.Flow
	Sequence      int64
	streamFactory interface {
		New(ctx context.Context, netFlow, portFlow gopacket.Flow, p gopacket.Layer, ac AssemblerContext) Stream
	}
	Stream interface {
		Accept(p gopacket.Layer, ci gopacket.CaptureInfo, dir FlowDirection, nextSeq Sequence, start *bool, ac AssemblerContext) bool
		Clean()
	}
)

func (k *Key) String() string {
	return fmt.Sprintf("%s:%s", k[0], k[1])
}

func (k *Key) Reverse() Key {
	return Key{
		k[0].Reverse(),
		k[1].Reverse(),
	}
}

// NewAssembler returns a stream aware packet fragment assembler.
func NewAssembler(s *streamPool) *assembler {
	s.mu.Lock()
	s.users++
	s.mu.Unlock()

	return &assembler{
		pool: s,
	}
}

// AssemblerContext provides method to get metadata.
type AssemblerContext interface {
	GetCaptureInfo() gopacket.CaptureInfo
}

// Implements AssemblerContext for Assemble().
type assemblerSimpleContext gopacket.CaptureInfo

func (asc *assemblerSimpleContext) GetCaptureInfo() gopacket.CaptureInfo {
	return gopacket.CaptureInfo(*asc)
}

// Assemble calls AssembleWithContext with the current timestamp, useful for
// packets being read directly off the wire.
func (a *assembler) Assemble(ctx context.Context, netFlow, portFlow gopacket.Flow, p *eqOldPacket.OldEQOuter) (*eqOldPacket.OldEQOuter, Stream, error) {
	cx := assemblerSimpleContext(gopacket.CaptureInfo{Timestamp: time.Now()})

	return a.AssembleWithContext(ctx, netFlow, portFlow, p, &cx)
}

type assemblerAction struct {
	nextSeq Sequence
	queue   bool
}

// Assemble is a reassembler for fragmented Old style packets.
func (a *assembler) AssembleWithContext(ctx context.Context, netFlow, portFlow gopacket.Flow, p *eqOldPacket.OldEQOuter, ac AssemblerContext) (*eqOldPacket.OldEQOuter, Stream, error) {
	ci := ac.GetCaptureInfo()
	timestamp := ci.Timestamp

	key := Key{netFlow, portFlow}
	pool := a.pool

	conn := pool.getConnection(ctx, key, false, p, ac)
	if conn == nil {
		return nil, nil, nil
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.lastSeen.Before(timestamp) {
		conn.lastSeen = timestamp
	}

	if !conn.stream.Accept(p, ci, conn.dir, conn.nextSeq, &a.start, ac) {
		return nil, nil, nil
	}

	if conn.closed {
		return nil, nil, nil
	}

	p, err := a.handleFrag(conn, p, timestamp)

	return p, conn.stream, err
}

func (a *assembler) handleFrag(conn *connection, p *eqOldPacket.OldEQOuter, t time.Time) (*eqOldPacket.OldEQOuter, error) {
	if !p.HasFlag(eqOldPacket.Frag) {
		return p, nil
	}

	fh := p.FragHeader

	id := fh.FragSEQ
	me := fh.Curr
	ttl := fh.Total

	frag, ok := conn.frags[id]
	if !ok {
		frag = &fragItem{
			origin: t,
			frags:  make([]fragment, ttl),
			seen:   0,
		}
		conn.frags[id] = frag
	}

	if frag.frags[me].payload == nil {
		frag.frags[me] = fragment{payload: p.Payload}
		frag.seen++
	}

	if frag.seen == uint16(len(frag.frags)) {
		var b bytes.Buffer
		for _, v := range frag.frags {
			b.Write(v.payload)
		}

		p.Payload = b.Bytes()

		delete(conn.frags, id)

		return p, nil
	}

	return nil, nil
}
