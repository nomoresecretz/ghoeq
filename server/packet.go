package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	"github.com/nomoresecretz/ghoeq/server/fakePcap"
)

var (
	capTimeout  = 250 * time.Millisecond
	promiscuous = true
	immediate   = true
	bufSize     = 4096 // I doubt we're gonna see jumbo packets.
)

type capture struct {
	s *gopacket.PacketSource
}

// NewCapture returns a capture object capable of decoding client streams.
func NewCapture(h pcapHandle) *capture {
	layers.RegisterUDPPortLayerType(6000, eqOldPacket.OldEQOuterType)
	layers.RegisterUDPPortLayerType(9000, eqOldPacket.OldEQOuterType)

	for i := range 400 {
		layers.RegisterUDPPortLayerType(layers.UDPPort(7000+i), eqOldPacket.OldEQOuterType)
	}

	return &capture{s: gopacket.NewPacketSource(h, h.LinkType())}
}

// Packets returns a packets channel. To eventually be replaced by a ring buffer system.
func (c *capture) Packets(ctx context.Context) chan gopacket.Packet {
	return c.s.PacketsCtx(ctx)
}

func liveHandle(d string) (*pcap.Handle, error) {
	h, err := pcap.NewInactiveHandle(d)
	if err != nil {
		return nil, err
	}
	defer h.CleanUp()

	if err = h.SetBufferSize(bufSize); err != nil {
		return nil, err
	}

	if err = h.SetTimeout(capTimeout); err != nil {
		return nil, err
	}

	if err = h.SetImmediateMode(immediate); err != nil {
		return nil, err
	}

	if err = h.SetPromisc(promiscuous); err != nil {
		return nil, err
	}

	return h.Activate()
}

func fileHandle(f string) (pcapHandle, error) {
	return fakePcap.New(f)
}

// GetSources returns the list of captureable interfaces.
func GetSources() ([]pcap.Interface, error) {
	return pcap.FindAllDevs()
}

func getHandle(src string) (pcapHandle, error) {
	if file, present := strings.CutPrefix(src, "file://"); present {
		return fileHandle(file)
	}

	h, err := liveHandle(src) // TODO: Support other kinds of captures here.
	if err != nil {
		return nil, fmt.Errorf("failed to open live handle: %w", err)
	}

	return h, nil
}
