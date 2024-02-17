package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	pb "github.com/nomoresecretz/ghoeq/common/proto/ghoeq"
)

var (
	// cFile    = flag.String("file", "", "Prior capture file")
	opMap    = flag.String("opFile", "opcodes.txt", "File with opcode mappings")
	port     = flag.Uint("port", 6420, "port to listen on for connections")
	bindAddr = flag.String("bindAddr", "", "Network bind address")
)

// arrayFlags is used as a multivalue input.
type arrayFlags []string

func (i *arrayFlags) String() string {
	return "my string representation"
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

var cDev arrayFlags

// Basic packet processing cmdline app to get started

func main() {
	flag.Var(&cDev, "device", "Device(s) to capture")
	flag.Parse()
	err := doStuff(context.Background())
	if err != nil {
		slog.Error("failed to start server", "error", err)
		os.Exit(-1)
	}
}

// doStuff does the actual heavy lifting running the server.
func doStuff(ctx context.Context) error {
	if len(cDev) == 0 {
		return fmt.Errorf("please specify allowed capture interfaces, ideally just 1")
	}
	ctx, ctxcf := context.WithCancel(ctx)
	defer ctxcf()
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bindAddr, *port))
	if err != nil {
		return err
	}
	var ops []grpc.ServerOption
	grpc := grpc.NewServer(ops...)
	gs, err := NewGhoeqServer(ctx)
	if err != nil {
		return err
	}
	pb.RegisterBackendServerServer(grpc, gs)

	cctx, _ := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	eg, wctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		for {
			select {
			case <-cctx.Done():
				gs.GracefulStop()
				grpc.GracefulStop()
				ctxcf()
			case <-wctx.Done():
				return nil
			}
		}
	})
	eg.Go(func() error {
		return gs.Go(wctx)
	})

	eg.Go(func() error {
		slog.Info("Starting server")
		return grpc.Serve(l)
	})

	return eg.Wait()
}
