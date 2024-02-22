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
	raw        = flag.Bool("raw", false, "raw capture mode")
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
	GetOp(decoder.OpCode) string
	GetOpByName(string) decoder.OpCode
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

	switch {
	case *raw:
		return followSource(ctx, c, d)
	default:
		return followClient(ctx, c, d)
	}
}

func followClient(ctx context.Context, c pb.BackendServerClient, d dec) error {
	eg, gctx := errgroup.WithContext(ctx)

	newStream := func(s *pb.Stream) {
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

	// confirm we have a running capture.
	_, err := getSessionID(ctx, c)
	if err != nil {
		return err
	}

	eg.Go(func() error {
		var client *pb.Client
		cs, err := c.AttachClient(gctx, &pb.AttachClientRequest{})
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
			if cli := c.GetClient(); client == nil && cli != nil {
				client = cli
				slog.Info("attached to new client", "address", client.Address, "ID", client.Id)
			}
			for _, s := range c.GetStreams() {
				newStream(s)
			}
		}
		return nil
	})

	return eg.Wait()
}

func followStream(ctx context.Context, stream *pb.Stream, c pb.BackendServerClient, d dec) error {
	var err error
	s, err := c.AttachStreamRaw(ctx, &pb.AttachStreamRequest{
		Id:        stream.GetId(),
		Nonce:     "0",
		SessionId: stream.GetSession().GetId(),
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
		op := d.GetOp(decoder.OpCode(opRaw))
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
	session := s.GetSessions()[0]
	slog.Info("identified capture session", "session", session)
	return session.GetId(), nil
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

func followSource(ctx context.Context, c pb.BackendServerClient, d dec) error {
	// confirm we have a running capture.
	sessionId, err := getSessionID(ctx, c)
	if err != nil {
		return err
	}

	streamInfo := make(map[string]*pb.Stream)
	getStreamInfo := func(ctx context.Context, streamId string) (*pb.Stream, error) {
		if s, ok := streamInfo[streamId]; ok {
			return s, nil
		}
		s, err := getStream(ctx, c, sessionId, streamId)
		if err != nil {
			return nil, err
		}
		streamInfo[streamId] = s
		return s, nil
	}

	slog.Info("identified capture session", "session", sessionId)
	stream, err := c.AttachSessionRaw(ctx, &pb.AttachSessionRequest{
		SessionId: sessionId,
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
		op := d.GetOp(decoder.OpCode(opRaw))
		if op == "" {
			op = fmt.Sprintf("%#4x", opRaw)
		}

		stream, err := getStreamInfo(ctx, p.StreamId)
		if err != nil {
			return err
		}

		if err != nil {
			return fmt.Errorf("failed to get stream info: %w", err)
		}

		fmt.Printf("StreamInfo %s %s %s:%s->%s:%s\n", stream.GetType(), stream.GetDirection().String(), stream.GetAddress(), stream.GetPort(), stream.GetPeerAddress(), stream.GetPeerPort())
		fmt.Printf("Packet %#4X : OpCode %s %s", 0x0, op, spew.Sdump(p.GetData()))
	}

	return nil
}

func getStream(ctx context.Context, c pb.BackendServerClient, sessionId, streamId string) (*pb.Stream, error) {
	r, err := c.ListStreams(ctx, &pb.ListStreamRequest{SessionId: sessionId, StreamId: streamId})
	if err != nil {
		return nil, err
	}

	streams := r.GetStreams()
	l := len(streams)
	if l != 1 {
		return nil, fmt.Errorf("incorrect stream count: got %d, want 1", l)
	}

	return streams[0], nil
}
