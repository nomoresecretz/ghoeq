package eqOldPacket

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gopacket/gopacket"
)

// Create custom layer structure
type OldEQOuter struct {
	Flags      OldFlags
	Seq      uint16
	ARSP     uint16
	ARQ      uint16
	FragHeader FragHeader
	AsqHigh    uint8
	AsqLow     uint8
	OpCode   []byte
	Payload    []byte
	CRC        uint32
}

type EQApplication struct {
	OpCode  uint16
	Payload []byte
}

type FragHeader struct {
	FragSEQ uint16
	Curr    uint16
	Total   uint16
}

type OldFlags uint16

func (f OldFlags) HasFlag(c OldFlags) bool {
	return f&c == c
}

func (f OldFlags) String() string {
	switch f {
	case SpecARQ:
		return "SpecARQ"
	case ARSP:
		return "ARSP"
	case ARQ:
		return "ARQ"
	case Closing, Closing2:
		return "Closing"
	case Frag:
		return "Fragment"
	case ASQ:
		return "ASQ"
	case SEQStart:
		return "SEQStart"
	case SEQEnd:
		return "SEQEnd"
	case Unk02, Unk08, Unk11, Unk12, Unk14, Unk18, Unk21:
		return "Unknown"
	}

	var s []string
	for k := SpecARQ; k < 16; k <<= 1 {
		if f&k != 0 {
			s = append(s, k.String())
		}
	}
	return strings.Join(s, "|")
}

const (
	SpecARQ OldFlags = 1 << iota
	Unk02
	ARSP
	Unk08
	Unk11
	Unk12
	Unk14
	Unk18
	Unk21
	ARQ
	Closing
	Frag
	ASQ
	SEQStart
	Closing2
	SEQEnd
)

// Register the layer type so we can use it
// The first argument is an ID. Use negative
// or 2000+ for custom layers. It must be unique
var OldEQOuterType = gopacket.RegisterLayerType(
	2010,
	gopacket.LayerTypeMetadata{
		Name:    "OldEQOuterType",
		Decoder: gopacket.DecodeFunc(DecodeOldEQOuter),
	},
)

var EQApplicationType = gopacket.RegisterLayerType(
	2011,
	gopacket.LayerTypeMetadata{
		Name:    "EQApplicationType",
		Decoder: gopacket.DecodeFunc(DecodeEQApp),
	},
)

func (l *EQApplication) LayerType() gopacket.LayerType {
	return EQApplicationType
}

func (l *EQApplication) LayerContents() []byte {
	var b []byte
	b = append(b, l.Payload...)
	return b
}

func (l *EQApplication) LayerPayload() []byte {
	return l.Payload
}

// When we inquire about the type, what type of layer should
// we say it is? We want it to return our custom layer type
func (l *OldEQOuter) LayerType() gopacket.LayerType {
	return OldEQOuterType
}

// LayerContents returns the information that our layer
// provides. In this case it is a header layer so
// we return the header information
func (l *OldEQOuter) LayerContents() []byte {
	var b []byte
	binary.BigEndian.AppendUint16(b, uint16(l.Flags))
	binary.BigEndian.AppendUint16(b, l.Seq)
	b = append(b, l.OpCode...)
	binary.BigEndian.AppendUint32(b, l.CRC)
	return b
}

// LayerPayload returns the subsequent layer built
// on top of our layer or raw payload
func (l *OldEQOuter) LayerPayload() []byte {
	return l.Payload
}

// Dumper is a basic string outputter for the packet for debugging.
func (l *OldEQOuter) Dumper() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Flags: %s ", l.Flags)
	fmt.Fprintf(&b, "Seq: %d ", l.Seq)

	if l.HasFlag(ARSP) {
		fmt.Fprintf(&b, "dwARSP: %d ", l.ARSP)
	}
	if l.HasFlag(ARQ) {
		fmt.Fprintf(&b, "dwARQ: %d ", l.ARQ)
	}

	if (l.FragHeader != FragHeader{}) {
		fmt.Fprintf(&b, "Frag{ %s } ", l.FragHeader.String())
	}

	if l.HasFlag(ASQ) {
		fmt.Fprintf(&b, "ASQ_Hi: %d ", l.AsqHigh)
		if l.HasFlag(ARQ) {
			fmt.Fprintf(&b, "ASQ_Lo: %d ", l.AsqLow)
		}
	}

	if !l.Ack() {
		fmt.Fprintf(&b, "OpCode: %d ", l.OpCode)
	}

	fmt.Fprintf(&b, "CRC: %#8x", l.CRC)
	return b.String()
}

func (fh *FragHeader) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "dwFragSEQ: %d ", fh.FragSEQ)
	fmt.Fprintf(&b, "dwCurr: %d ", fh.Curr)
	fmt.Fprintf(&b, "dwTotal: %d", fh.Total)
	return b.String()
}

func (l *OldEQOuter) Ack() bool {
	// TODO
	return false
}

func (l *OldEQOuter) HasFlag(f OldFlags) bool {
	return l.Flags.HasFlag(f)
}

// decodeOldPacket does the basic lifting of processing an old style data packet.
func decodeOldPacket(data []byte) (*OldEQOuter, error) {
	next := 0
	p := &OldEQOuter{}
	p.Flags = OldFlags(binary.BigEndian.Uint16(data[0:2]))
	p.Seq = binary.BigEndian.Uint16(data[2:4])
	next += 4
	if p.HasFlag(ARSP) {
		p.ARSP = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
	}
	if p.HasFlag(ARQ) {
		p.ARQ = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
	}
	if p.HasFlag(Frag) {
		fr := FragHeader{}
		fr.FragSEQ = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
		fr.Curr = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
		fr.Total = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
		p.FragHeader = fr
	}

	if p.HasFlag(ASQ) {
		p.AsqHigh = data[next]
		next += 1
		if p.HasFlag(ARQ) {
			p.AsqLow = data[next]
			next += 1
		}
	}

	p.CRC = binary.BigEndian.Uint32(data[len(data)-4:])

	if p.HasFlag(Frag) {
		f := p.FragHeader
		slog.Debug("fragment", "cur", f.Curr, "ttl", f.Total, "seq", f.FragSEQ)
	}
	remain := len(data) - next - 4
	adj := 0
	if remain >= 2 && (!p.HasFlag(Frag) || p.FragHeader.Curr == 0) {
		p.OpCode = append(p.OpCode, data[next:next+2]...)
		if p.OpCode[0] == 0x5f && p.OpCode[1] == 0x41 {
			slog.Debug(p.Dumper())
		}
		next += 2
		adj = 2
		remain -= 2
	}
	if remain > 0 {
		pl := (len(data) - 4)
		p.Payload = data[next-adj : pl]
		next += pl - next
	}

	next += 4
	if next != len(data) {
		return nil, fmt.Errorf("invalid packet size, got %d, want %d", next, len(data))
	}
	return p, nil
}

func decodeEQAppPacket(data []byte) (*EQApplication, error) {
	p := &EQApplication{}

	p.OpCode = binary.BigEndian.Uint16(data)

	if len(data) <= 2 {
		return p, nil
	}
	p.Payload = data[2:]
	return p, nil
}

// DecodeOldEQOuter processes the raw format of orignal protocol client packets.
func DecodeOldEQOuter(data []byte, p gopacket.PacketBuilder) error {
	if l, err := decodeOldPacket(data); err != nil {
		return err
	} else {
		p.AddLayer(l)
	}
	return p.NextDecoder(gopacket.LayerTypePayload)
}

// DecodeEQApp handles the inner client application packets.
func DecodeEQApp(data []byte, p gopacket.PacketBuilder) error {
	if l, err := decodeEQAppPacket(data); err != nil {
		return err
	} else {
		p.AddLayer(l)
	}
	return p.NextDecoder(gopacket.LayerTypePayload)
}
