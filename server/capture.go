package main

import (
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

var (
	capTimeout  = 1 * time.Second
	promiscuous = true
)

type capture struct {
	s *gopacket.PacketSource
}

//NewCapture returns a capture object capable of decoding client streams.
func NewCapture(h *pcap.Handle) *capture {
	layers.RegisterUDPPortLayerType(6000, OldEQOuterType)
	layers.RegisterUDPPortLayerType(9000, OldEQOuterType)
	for i := range 400 {
		layers.RegisterUDPPortLayerType(layers.UDPPort(7000+i), OldEQOuterType)
	}
	return &capture{s: gopacket.NewPacketSource(h, h.LinkType())}
}

//Packets returns a packets channel. To eventually be replaced by a ring buffer system.
func (c *capture) Packets() chan gopacket.Packet {
	return c.s.Packets()
}

func liveHandle(d string) (*pcap.Handle, error) {
	return pcap.OpenLive(d, 1024, promiscuous, capTimeout)
}

func fileHandle(f string) (*pcap.Handle, error) {
	return pcap.OpenOffline(f)
}
