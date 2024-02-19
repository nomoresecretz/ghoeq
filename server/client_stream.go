package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gopacket/gopacket"

	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/common/ringbuffer"
	"github.com/nomoresecretz/ghoeq/server/assembler"
)

var ocList = map[string]struct{}{
	"OP_LoginPC":       {},
	"OP_LoginAccepted": {},
	"OP_SendLoginInfo": {},
	"OP_LogServer": {},
	"OP_ZoneEntry": {},
	"OP_PlayerProfile": {},
}

type streamFactory struct {
	mgr  *streamMgr
	cout chan<- streamPacket
	oc   map[string]uint16
}

type streamType uint

func (s streamType) String() string {
	switch s {
	case ST_LOGIN:
		return "ST_LOGIN"
	case ST_WORLD:
		return "ST_WORLD"
	case ST_ZONE:
		return "ST_ZONE"
	default:
		return "ST_UNKNOWN"
	}
}

const (
	ST_UNKNOWN streamType = iota
	ST_LOGIN              = 1
	ST_WORLD              = 2
	ST_ZONE               = 3
	ST_CHAT               = 4
)

type stream struct {
	key   assembler.Key
	net   gopacket.Flow
	port  gopacket.Flow
	dir   assembler.FlowDirection
	rb    *ringbuffer.RingBuffer[*eqOldPacket.EQApplication]
	sType streamType
	sf    *streamFactory
}

const (
	packetBufferSize = 20
)

type streamPacket struct {
	stream *stream
	packet *eqOldPacket.EQApplication
	seq    uint64
}

func (sf *streamFactory) New(netFlow, portFlow gopacket.Flow, l gopacket.Layer, ac assembler.AssemblerContext) assembler.Stream {
	s := &stream{
		key:  assembler.Key{netFlow, portFlow},
		net:  netFlow,
		port: portFlow,
		rb:   ringbuffer.New[*eqOldPacket.EQApplication](100),
		sf:   sf,
	}

	return s
}

func (s *stream) Accept(p gopacket.Layer, ci gopacket.CaptureInfo, dir assembler.FlowDirection, nSeq assembler.Sequence, start *bool, ac assembler.AssemblerContext) bool {
	return true
}

func NewStreamFactory(sm *streamMgr, cout chan<- streamPacket) *streamFactory {
	oc := make(map[string]uint16)
	for k := range ocList {
		oc[k] = sm.decoder.GetOpByName(k)
	}

	return &streamFactory{
		cout: cout,
		mgr:  sm,
		oc:   oc,
	}
}

func (s *stream) Send(ctx context.Context, l gopacket.Layer) error {
	p, ok := l.(*eqOldPacket.EQApplication)
	if !ok {
		return fmt.Errorf("improper packet type %t", l)
	}

	s.rb.Add(p)

	return s.HandleAppPacket(ctx, p)
}

func (s *stream) HandleAppPacket(ctx context.Context, p *eqOldPacket.EQApplication) error {
	select {
	case <-ctx.Done():
	case s.sf.cout <- streamPacket{
		seq:    0, // TODO: pipe in the seq numbers.
		stream: s,
		packet: p,
	}:
	default:
		slog.Error("failed to send packet to fanout")
	}
	
	return nil
}

func (s *stream) Identify(ctx context.Context, p *eqOldPacket.EQApplication) {
	switch p.OpCode {
	case s.sf.oc["OP_LoginPC"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_LOGIN
	case s.sf.oc["OP_LoginAccepted"]:
		s.dir = assembler.DirServerToClient
		s.sType = ST_LOGIN
	case s.sf.oc["OP_SendLoginInfo"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_WORLD
	case s.sf.oc["OP_LogServer"]:
		s.dir = assembler.DirServerToClient
		s.sType = ST_WORLD
	case s.sf.oc["OP_ZoneEntry"]:
		s.dir = assembler.DirClientToServer
		s.sType = ST_ZONE
	case s.sf.oc["OP_PlayerProfile"]:
		s.dir = assembler.DirServerToClient
		s.sType = ST_ZONE
	}
}
