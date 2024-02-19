package main

import (
	"context"
	"fmt"
	"log/slog"

	pb "github.com/nomoresecretz/ghoeq/common/proto/ghoeq"
	"golang.org/x/sync/errgroup"
)

type ghoeqServer struct {
	allowedDev map[string]struct{}

	sMgr *sessionMgr
	pb.UnimplementedBackendServerServer
}

func NewGhoeqServer(ctx context.Context) (*ghoeqServer, error) {
	allowedDev := make(map[string]struct{})
	
	for _, d := range cDev {
		if d != "" {
			allowedDev[d] = struct{}{}
		}
	}

	slog.Info("allowed device list", "allowed_devs", allowedDev)
	
	return &ghoeqServer{
		allowedDev: allowedDev,
	}, nil
}

func (s *ghoeqServer) validSource(src string) bool {
	_, v := s.allowedDev[src]

	return v || len(s.allowedDev) == 0
}

func (s *ghoeqServer) Go(ctx context.Context) error {
	g, wctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		s.sMgr = NewSessionManager()
		
		return s.sMgr.Go(wctx, s)
	})

	return g.Wait()
}

func (s *ghoeqServer) ListSources(ctx context.Context, r *pb.ListRequest) (*pb.ListSourcesResponse, error) {
	sl, err := GetSources()
	if err != nil {
		return nil, err
	}
	var rs []*pb.Source

	l := len(s.allowedDev)
	for _, i := range sl {
		if !s.validSource(i.Name) && l > 0 {
			continue
		}
		// TODO: consider adding address info
		rs = append(rs, &pb.Source{
			Id:          i.Name,
			Description: i.Description,
		})
	}

	return &pb.ListSourcesResponse{
		Sources: rs,
	}, nil
}

// ListSession lists the current active capture sessions.
func (s *ghoeqServer) ListSession(ctx context.Context, p *pb.ListRequest) (*pb.ListSessionResponse, error) {
	var sessions []*pb.Session

	s.sMgr.muSessions.RLock()
	defer s.sMgr.muSessions.RUnlock()

	for id, session := range s.sMgr.sessions {
		sessions = append(sessions, &pb.Session{
			Id:     id.String(),
			Source: session.source,
		})
	}

	return &pb.ListSessionResponse{
		Sessions: sessions,
	}, nil
}

// ListStreams lists the current known client streams. A stream is 1 client session, but it can contain multiple independent substreams.
func (s *ghoeqServer) ListStreams(ctx context.Context, r *pb.ListRequest) (*pb.ListStreamsResponse, error) {
	return nil, fmt.Errorf("unimplemented")
}

func (s *ghoeqServer) ModifySession(ctx context.Context, r *pb.ModifySessionRequest) (*pb.SessionResponse, error) {
	for _, m := range r.Mods {
		if err := s.handleSessionRequest(ctx, m); err != nil {
			return nil, err
		}
	}

	return &pb.SessionResponse{}, nil
}

// AttachStreamRaw provides a single full client stream of decrypted but unprocessed EQApplication packets.
func (s *ghoeqServer) AttachStreamRaw(r *pb.AttachStreamRawRequest, stream pb.BackendServer_AttachStreamRawServer) error {
	ctx := stream.Context()
	// Get a pull channel
	sid := r.GetId()

	// TODO: attach to client stream, not generic capture session. AKA implement true client tracking as a middle layer.
	clientSession, err := s.sMgr.AttachToSession(ctx, sid)
	if err != nil {
		return err
	}

	defer clientSession.Close()

	// loop sending the packets to the client
	for {
		select {
		case <-ctx.Done():
			return nil
		case p, ok := <-clientSession.handle:
			if !ok {
				return nil
			}

			st := &pb.StreamThread{
				Port: p.stream.port.Src().String(),
				PeerPort: p.stream.port.Dst().String(),
				PeerAddress: p.stream.net.Dst().String(),
				Address: p.stream.net.Src().String(),
				Type: pb.PeerType(p.stream.sType),
				Direction: pb.Direction(p.stream.dir),
			}
			op := &pb.APPacket{
				Seq: p.seq,
				OpCode: uint32(p.packet.OpCode),
				Data:   p.packet.Payload,
				StreamInfo: st,
			}

			if err := stream.Send(op); err != nil {
				return err
			}
		}
	}
}

// GracefulStop cleanly shuts down the server closing out all operations as possible.
func (s *ghoeqServer) GracefulStop() {
	slog.Info("server shutdown requested")
	s.sMgr.GracefulStop()
}
