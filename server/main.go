package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"golang.org/x/sync/errgroup"
)

var (
	cDev  = flag.String("device", "", "Device to capture")
	cFile = flag.String("file", "", "Prior capture file")
	opMap = flag.String("opFile", "opcodes.txt", "File with opcode mappings")
)

// Basic packet processing cmdline app to get started

func main() {
	flag.Parse()
	err := doStuff(context.Background())
	if err != nil {
		slog.Error("failed to do x: %w", err)
		os.Exit(-1)
	}
}

//doStuff is a temporary wrapper for the proof of concept version. To be replaced with a proper gRPC service.
func doStuff(ctx context.Context) error {
	// Skeleton packet capture
	h, err := getHandle()
	if err != nil {
		return err
	}
	defer h.Close()

	apc := make(chan *EQApplication)
	s := NewStreamMgr()

	wg, wctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		return s.NewCapture(wctx, h, apc)
	})

	wg.Go(func() error {
		return processPackets(wctx, apc, s)
	})

	return wg.Wait()
}
