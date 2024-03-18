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

	"github.com/nomoresecretz/ghoeq/server"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/nomoresecretz/ghoeq-common/decoder"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
)

const defaultPort = 6420

var (
	opMap     = flag.String("opFile", "opcodes.txt", "File with opcode mappings")
	port      = flag.Uint("port", defaultPort, "port to listen on for connections")
	bindAddr  = flag.String("bindAddr", "127.0.0.1", "Network bind address")
	debugFlag = flag.Bool("debug", false, "enable debugging")
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

	if *debugFlag {
		opts := &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}
		h := slog.New(slog.NewTextHandler(os.Stdout, opts))
		slog.SetDefault(h)
	}

	err := doStuff(context.Background())
	if err != nil {
		slog.Error("failed to start server", "error", err)
		os.Exit(-1)
	}
}

// doStuff does the actual heavy lifting running the server.
func doStuff(ctx context.Context) error {
	ctx, ctxcf := context.WithCancel(ctx)
	defer ctxcf()

	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *bindAddr, *port))
	if err != nil {
		return err
	}
	defer l.Close()

	d := decoder.NewDecoder()
	if err := d.LoadMap(*opMap); err != nil {
		return fmt.Errorf("unable to load decode map: %w", err)
	}

	var ops []grpc.ServerOption
	grpc := grpc.NewServer(ops...)

	gs, err := server.New(ctx, cDev)
	if err != nil {
		return err
	}

	if len(cDev) == 0 {
		sl, err := gs.ListSources(ctx, &pb.ListRequest{})
		if err != nil {
			return err
		}

		slog.Error("please allow one or more of the following capture sources:")

		for _, s := range sl.Sources {
			slog.Error("source", "id", s.Id, "description", s.Description)
		}

		return fmt.Errorf("no source selected")
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

				return nil
			case <-wctx.Done():
				grpc.GracefulStop()

				return nil
			}
		}
	})

	eg.Go(func() error {
		return gs.Run(wctx, d, false)
	})

	eg.Go(func() error {
		slog.Info("Starting server")

		return grpc.Serve(l)
	})

	return eg.Wait()
}
