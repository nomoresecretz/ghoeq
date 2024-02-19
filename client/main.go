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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	serverAddr = flag.String("server", "127.0.0.1:6420", "Server target info")
	src        = flag.String("source", "", "server capture source")
	opFile     = flag.String("opFile", "opcodes.txt", "opcode mapping data file")
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

func doStuff(ctx context.Context) error {
	conn, err := grpc.DialContext(ctx, *serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	dec := decoder.NewDecoder()
	if *opFile != "" {
		if err := dec.LoadMap(*opFile); err != nil {
			return err
		}
	}
	c := pb.NewBackendServerClient(conn)

	// Until auto sessions are in, we have to do the grunt work.
	streamid, err := getSessionID(ctx, c)
	if err != nil {
		return err
	}

	slog.Info("identified capture session", "session", streamid)
	stream, err := c.AttachStreamRaw(ctx, &pb.AttachStreamRawRequest{
		Id:    streamid,
		Nonce: "0",
	})
	if err != nil {
		return err
	}
	slog.Info("connected, beginning stream")
	for {
		p, err := stream.Recv()
		if err == io.EOF {
			slog.Info("server ended stream")
			break
		}
		if err != nil {
			return err
		}
		opRaw := p.GetOpCode()
		op := dec.GetOp(uint16(opRaw))
		if op == "" {
			op = fmt.Sprintf("%#4x", opRaw)
		}
		// TODO: move opcode decoder to independent common module. Add api to push/pull from server.
		si := p.GetStreamInfo()
		fmt.Printf("StreamInfo %s %s %s:%s->%s:%s\n", si.GetType().String(), si.GetDirection(), si.GetAddress(), si.GetPort(), si.GetPeerAddress(), si.GetPeerPort())
		fmt.Printf("Packet %#4X : OpCode %s %s\n", 0x0, op, spew.Sdump(p.GetData()))
	}

	return nil
}

func getSessionID(ctx context.Context, c pb.BackendServerClient) (string, error) {
	s, err := c.ListSession(ctx, &pb.ListRequest{})
	if err != nil {
		return "", err
	}
	if len(s.Sessions) == 0 {
		src, err := getSource(ctx, c)
		if err != nil {
			return "", err
		}
		_, err = c.ModifySession(ctx, &pb.ModifySessionRequest{
			Nonce: "0",
			Mods: []*pb.ModifyRequest{
				{
					State:  pb.State_STATE_START,
					Source: src,
				},
			},
		})
		if err != nil {
			return "", err
		}
		s, err = c.ListSession(ctx, &pb.ListRequest{})
		if err != nil {
			return "", err
		}
	}
	switch l := len(s.GetSessions()); {
	case l == 0:
		return "", fmt.Errorf("no capture session avaliable")
	case l > 1:
		return "", fmt.Errorf("too many active sessions to pick one. select manually")
	}
	return s.GetSessions()[0].GetId(), nil
}

func getSource(ctx context.Context, c pb.BackendServerClient) (string, error) {
	if *src != "" {
		return *src, nil
	}
	s, err := c.ListSources(ctx, &pb.ListRequest{})
	if err != nil {
		return "", err
	}
	sources := s.GetSources()
	if len(sources) != 1 {
		return "", fmt.Errorf("too many sources, pick one manually")
	}
	return sources[0].GetId(), nil
}
