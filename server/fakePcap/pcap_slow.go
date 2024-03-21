package fakePcap

import (
	"fmt"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

var maxFakeWait = 1 * time.Second

type fakePcap struct {
	h     *pcap.Handle
	start time.Time
	diff  time.Duration
}

func New(f string) (*fakePcap, error) {
	pc, err := pcap.OpenOffline(f)
	if err != nil {
		return nil, fmt.Errorf("failed to open file handle: %w", err)
	}

	return &fakePcap{
		h: pc,
	}, nil
}

func (p *fakePcap) Close() {
	p.h.Close()
}

func (p *fakePcap) LinkType() layers.LinkType {
	return p.h.LinkType()
}

func (p *fakePcap) ReadPacketData() (data []byte, ci gopacket.CaptureInfo, err error) {
	d, ci, err := p.h.ReadPacketData()

	if p.start.IsZero() {
		p.start = time.Now()
		p.diff = p.start.Sub(ci.Timestamp)
	}

	delta := time.Until(ci.Timestamp.Add(p.diff))
	if delta > maxFakeWait {
		delta = maxFakeWait
	}

	time.Sleep(delta)

	return d, ci, err
}

func (p *fakePcap) SetBPFFilter(s string) error {
	return p.h.SetBPFFilter(s)
}
