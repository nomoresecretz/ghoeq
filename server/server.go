package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	pb "github.com/nomoresecretz/ghoeq/common/proto/ghoeq"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
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

	for _, session := range s.sMgr.sessions {
		sessions = append(sessions, session.Proto())
	}

	return &pb.ListSessionResponse{
		Sessions: sessions,
	}, nil
}

// ListStreams lists the current known client streams. A stream is 1 client session, but it can contain multiple independent substreams.
func (s *ghoeqServer) ListStreams(ctx context.Context, r *pb.ListStreamRequest) (*pb.ListStreamsResponse, error) {
	sess, err := s.getSession(r.GetSessionId())
	if err != nil {
		return nil, err
	}

	sess.mu.RLock()
	streamId := r.GetStreamId()

	var streams []*pb.Stream
	for _, str := range sess.sm.clientStreams {
		if streamId != "" && streamId != str.key.String() {
			continue
		}
		streams = append(streams, str.Proto())
	}
	sess.mu.RUnlock()

	return &pb.ListStreamsResponse{Streams: streams}, nil
}

func (s *ghoeqServer) ModifySession(ctx context.Context, r *pb.ModifySessionRequest) (*pb.SessionResponse, error) {
	for _, m := range r.Mods {
		if err := s.handleSessionRequest(ctx, m); err != nil {
			return nil, err
		}
	}

	return &pb.SessionResponse{}, nil
}

// AttachClient notifies of new streams for a given client track.
func (s *ghoeqServer) AttachClient(r *pb.AttachClientRequest, stream pb.BackendServer_AttachClientServer) error {
	ctx := stream.Context()

	cli, err := s.sMgr.clientWatch.WaitForClient(ctx, r.GetClientId())
	if err != nil {
		return err
	}

	var cupd *pb.Client
	if r.GetClientId() == "" {
		cupd = &pb.Client{
			Id: string(cli.id.String()),
			// Address: cli., // TODO: add client address logic.
		}
	}
	r1 := &pb.ClientUpdate{
		Client: cupd,
	}
	slog.Debug("new client identified, notifying watchers")

	seen := make(map[string]struct{})

	// Read lock the client, then grab the notice channel, and all the streams in one go before unlock.
	var p chan struct{}
	cli.mu.RLock()
	p = cli.ping

	var strz []*pb.Stream
	for _, v := range cli.streams {
		streamProto := v.Proto()
		streamProto.Session = v.sf.mgr.session.Proto()
		strz = append(strz, streamProto)
		seen[v.id] = struct{}{}
	}
	cli.mu.RUnlock()

	r1.Streams = strz
	if err := stream.Send(r1); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p:
			p = cli.PingChan()

			var strz []*pb.Stream

			for _, v := range cli.streams {
				streamProto := v.Proto()
				if _, ok := seen[v.id]; ok {
					continue
				}
				slog.Debug("notifying watcher of new gameClient stream")
				streamProto.Session = v.sf.mgr.session.Proto()
				strz = append(strz, streamProto)
				seen[v.id] = struct{}{}
			}
			stream.Send(&pb.ClientUpdate{Streams: strz})
		}
	}
}

func (s ghoeqServer) getSession(sessionId string) (*session, error) {
	sId, err := uuid.Parse(sessionId)
	if err != nil {
		return nil, fmt.Errorf("invalid id format: %w", err)
	}

	return s.sMgr.SessionById(sId)
}

// AttachStreamRaw provides a single full client stream of decrypted but unprocessed EQApplication packets.
func (s *ghoeqServer) AttachStreamRaw(r *pb.AttachStreamRequest, stream pb.BackendServer_AttachStreamRawServer) error {
	ctx := stream.Context()

	ses, err := s.getSession(r.GetSessionId())
	if err != nil {
		return err
	}

	str, err := ses.sm.StreamById(r.GetId())
	if err != nil {
		return err
	}

	cStream, err := str.AttachToStream(ctx)
	if err != nil {
		return err
	}

	slog.Debug("client added a stream watch")

	defer cStream.Close()

	// send the backlog of packets seen before they attached.
	op := str.rb.GetAll()
	slog.Debug("sending packet backlog")

	seen := make(map[uint64]struct{})
	for _, p := range op {
		seen[p.seq] = struct{}{}
		outP := p.Proto()
		if err := stream.Send(outP); err != nil {
			return err
		}
	}

	slog.Debug("looping new packets")

	return s.sendLoop(ctx, cStream.handle, stream, seen)
}

// AttachSessionRaw provides a raw feed of a capture session app packets. Mostly intended for debugging.
func (s *ghoeqServer) AttachSessionRaw(r *pb.AttachSessionRequest, stream pb.BackendServer_AttachSessionRawServer) error {
	ctx := stream.Context()

	ses, err := s.getSession(r.GetSessionId())
	if err != nil {
		return err
	}

	cStream, err := ses.AddClient(ctx)
	if err != nil {
		return err
	}

	slog.Debug("client added a stream watch")

	defer cStream.Close()

	return s.sendLoop(ctx, cStream.handle, stream, nil)
}

type streamSender interface {
	Send(*pb.APPacket) error
	grpc.ServerStream
}

func (s *ghoeqServer) sendLoop(ctx context.Context, handle <-chan StreamPacket, stream streamSender, seen map[uint64]struct{}) error {
	// loop sending the packets to the client
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-handle:
			if !ok {
				return nil
			}

			// Avoid double send
			if seen != nil {
				if _, ok := seen[p.seq]; ok {
					continue
				}
				seen[p.seq] = struct{}{}
				delete(seen, p.seq-100)
			}

			op := p.Proto()

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
