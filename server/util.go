package main

import (
	"errors"

	"github.com/gopacket/gopacket/pcap"
)

//getHandle is just a simple request handler for getting the data handle. To be replaced.
func getHandle() (*pcap.Handle, error) {
	if *cDev != "" {
		return liveHandle(*cDev)
	}
	if *cFile != "" {
		return fileHandle(*cFile)
	}
	return nil, errors.New("no capture source")
}
