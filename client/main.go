package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/nomoresecretz/ghoeq/common/decoder"
	pb "github.com/nomoresecretz/ghoeq/common/proto/ghoeq"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	serverAddr = flag.String("server", "127.0.0.1:6420", "Server target info")
	src        = flag.String("source", "", "server capture source")
	opFile     = flag.String("opFile", "", "opcode mapping data file")
)

// Simple rpc test client
func main() {
	flag.Parse()
	err := doStuff(context.Background())
	if err != nil {
		slog.Error("failed to do x: %w", err)
		os.Exit(-1)
	}
}

type dec interface {
	GetOp(uint16) string
	GetOpByName(string) uint16
	LoadMap(string) error
}

func doStuff(ctx context.Context) error {
	conn, err := grpc.DialContext(ctx, *serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	d := decoder.NewDecoder()
	if *opFile != "" {
		if err := d.LoadMap(*opFile); err != nil {
			return err
		}
	}
	c := pb.NewBackendServerClient(conn)

	eg, gctx := errgroup.WithContext(ctx)

	newStream := func(s *pb.StreamThread) {
		slog.Info("new client stream",
		"type", s.GetType().String(),
		"dir", s.GetDirection(),
		"port", s.GetPort(),
		"peer_addr", s.GetPeerAddress(),
		"peer_port", s.GetPeerPort())
		eg.Go(func() error {
			return followStream(gctx, s, c, d)
		})
	}

	eg.Go(func() error {
		var client *pb.Client
		cs, err := c.AttachClient(ctx, &pb.AttachClientRequest{})
		if err != nil {
			return err
		}
		for {
			c, err := cs.Recv()
			if err == io.EOF {
				slog.Info("server/client ended stream")
				break
			}
			if err != nil {
				return err
			}
			if cli := c.GetClient(); cli != nil {
				client = cli
				slog.Info("attached to new client", "address", client.Address, "ID", client.Id)
			}
			for _, s := range c.GetStreamThreads() {
				newStream(s)
			}
		}
		return nil
	})

	return eg.Wait()
}

func followStream(ctx context.Context, stream *pb.StreamThread, c pb.BackendServerClient, d dec) error {
	var err error
	s, err := c.AttachStreamRaw(ctx, &pb.AttachStreamRawRequest{
		Id:    stream.GetId(),
		Nonce: "0",
	})
	if err != nil {
		return err
	}
	sType := stream.GetType().String()
	sDir := stream.GetDirection()
	sAddr := stream.GetAddress()
	sPort := stream.GetPort()
	sPeerAddr := stream.GetPeerAddress()
	sPeerPort := stream.GetPeerPort()

	slog.Info("connected, beginning stream")
	for {
		p, err := s.Recv()
		if err == io.EOF {
			slog.Info("server ended stream")
			break
		}
		if err != nil {
			return err
		}
		opRaw := p.GetOpCode()
		op := d.GetOp(uint16(opRaw))
		if op == "" {
			op = fmt.Sprintf("%#4x", opRaw)
		}
		// TODO: Add api to push/pull opcode definitions from server.
		//		si := p.GetStreamInfo()
		fmt.Printf("StreamInfo %s %s %s:%s->%s:%s\n", sType, sDir, sAddr, sPort, sPeerAddr, sPeerPort)
		fmt.Printf("Packet %#4X : OpCode %s %s\n", 0x0, op, spew.Sdump(p.GetData()))
	}
	return nil
}
