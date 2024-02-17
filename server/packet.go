package main

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gopacket/gopacket"
)

type fragHeader struct {
	dwFragSEQ uint16
	dwCurr    uint16
	dwTotal   uint16
}

type OldFlags uint16

// Create custom layer structure
type OldEQOuter struct {
	Flags    OldFlags
	dwSeq    uint16
	dwARSP   uint16
	dwARQ    uint16
	fh       fragHeader
	asqHigh  uint8
	asqLow   uint8
	dwOpCode []byte
	Payload  []byte
	CRC      uint32
}

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
		Name: "OldEQOuterType",
		Decoder: gopacket.DecodeFunc(DecodeOldEQOuter),
	},
)

var EQApplicationType = gopacket.RegisterLayerType(
	2011,
	gopacket.LayerTypeMetadata{
		Name: "EQApplicationType",
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
	binary.BigEndian.AppendUint16(b, l.dwSeq)
	b = append(b, l.dwOpCode...)
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
	fmt.Fprintf(&b, "Seq: %d ", l.dwSeq)

	if l.hasFlag(ARSP) {
		fmt.Fprintf(&b, "dwARSP: %d ", l.dwARSP)
	}
	if l.hasFlag(ARQ) {
		fmt.Fprintf(&b, "dwARQ: %d ", l.dwARQ)
	}

	if (l.fh != fragHeader{}) {
		fmt.Fprintf(&b, "Frag{ %s } ", l.fh.String())
	}

	if l.hasFlag(ASQ) {
		fmt.Fprintf(&b, "ASQ_Hi: %d ", l.asqHigh)
		if l.hasFlag(ARQ) {
			fmt.Fprintf(&b, "ASQ_Lo: %d ", l.asqLow)
		}
	}

	if !l.ack() {
		fmt.Fprintf(&b, "OpCode: %d ", l.dwOpCode)
	}

	fmt.Fprintf(&b, "CRC: %#8x", l.CRC)
	return b.String()
}

func (fh *fragHeader) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "dwFragSEQ: %d ", fh.dwFragSEQ)
	fmt.Fprintf(&b, "dwCurr: %d ", fh.dwCurr)
	fmt.Fprintf(&b, "dwTotal: %d", fh.dwTotal)
	return b.String()
}

func (l *OldEQOuter) ack() bool {
	// TODO
	return false
}

func (l *OldEQOuter) hasFlag(f OldFlags) bool {
	return l.Flags.HasFlag(f)
}

// decodeOldPacket does the basic lifting of processing an old style data packet.
func decodeOldPacket(data []byte) (*OldEQOuter, error) {
	next := 0
	p := &OldEQOuter{}
	p.Flags = OldFlags(binary.BigEndian.Uint16(data[0:2]))
	p.dwSeq = binary.BigEndian.Uint16(data[2:4])
	next += 4
	if p.hasFlag(ARSP) {
		p.dwARSP = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
	}
	if p.hasFlag(ARQ) {
		p.dwARQ = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
	}
	if p.hasFlag(Frag) {
		fr := fragHeader{}
		fr.dwFragSEQ = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
		fr.dwCurr = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
		fr.dwTotal = binary.BigEndian.Uint16(data[next : next+2])
		next += 2
		p.fh = fr
	}

	if p.hasFlag(ASQ) {
		p.asqHigh = data[next]
		next += 1
		if p.hasFlag(ARQ) {
			p.asqLow = data[next]
			next += 1
		}
	}

	p.CRC = binary.BigEndian.Uint32(data[len(data)-4:])

	if p.hasFlag(Frag) {
		f := p.fh
		slog.Debug("fragment", "cur", f.dwCurr, "ttl", f.dwTotal, "seq", f.dwFragSEQ)
	}
	remain := len(data) - next - 4
	adj := 0
	if remain >= 2 && (!p.hasFlag(Frag) || p.fh.dwCurr == 0) {
		p.dwOpCode = append(p.dwOpCode, data[next:next+2]...)
		if p.dwOpCode[0] == 0x5f && p.dwOpCode[1] == 0x41 {
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

type EQApplication struct {
	OpCode  uint16
	Payload []byte
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
