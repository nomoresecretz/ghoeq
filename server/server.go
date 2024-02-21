package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
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
func (s *ghoeqServer) ListStreams(ctx context.Context, r *pb.ListStreamRequest) (*pb.ListStreamsResponse, error) {
	sID := r.GetSessionId()
	s.sMgr.muSessions.RLock()

	uuid, err := uuid.Parse(sID)
	if err != nil {
		return nil, fmt.Errorf("invalid session id format: %w", err)
	}

	sess, ok := s.sMgr.sessions[uuid]
	if !ok {
		s.sMgr.muSessions.RUnlock()
		return nil, fmt.Errorf("unknown session: %s", sID)
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

	var strz []*pb.Stream
	cli.mu.RLock()
	for _, v := range cli.streams {
		streamProto := v.Proto()
		session := v.sf.mgr.session
		streamProto.Session = &pb.Session{
			Id: session.id.String(),
		}
		strz = append(strz, streamProto)
		seen[streamProto.Id] = struct{}{}
	}
	cli.mu.RUnlock()

	r1.Streams = strz
	if err := stream.Send(r1); err != nil {
		return err
	}

	var p chan struct{}
	cli.mu.RLock()
	p = cli.ping
	cli.mu.RUnlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p:
			cli.mu.RLock()
			p = cli.ping
			cli.mu.RUnlock()

			var strz []*pb.Stream

			for _, v := range cli.streams {
				streamProto := v.Proto()
				if _, ok := seen[streamProto.Id]; ok {
					continue
				}
				slog.Debug("notifying watcher of new gameClient stream")
				session := v.sf.mgr.session
				streamProto.Session = &pb.Session{
					Id: session.id.String(),
				}
				strz = append(strz, streamProto)
				seen[streamProto.Id] = struct{}{}
			}
			stream.Send(&pb.ClientUpdate{Streams: strz})
		}
	}
}

// AttachStreamRaw provides a single full client stream of decrypted but unprocessed EQApplication packets.
func (s *ghoeqServer) AttachStreamRaw(r *pb.AttachStreamRawRequest, stream pb.BackendServer_AttachStreamRawServer) error {
	ctx := stream.Context()
	// Get a pull channel
	sid := r.GetSessionId()
	sId, err := uuid.Parse(sid)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}

	s.sMgr.muSessions.RLock()

	ses, ok := s.sMgr.sessions[sId]
	if !ok {
		s.sMgr.muSessions.RUnlock()
		return fmt.Errorf("unknown session: %s", sid)
	}
	s.sMgr.muSessions.RUnlock()
	ses.sm.mu.RLock()

	k, ok := ses.sm.streamMap[r.GetId()]
	if !ok {
		return fmt.Errorf("unknown stream id: %s", r.GetId())
	}

	str, ok := ses.sm.clientStreams[k]
	if !ok {
		return fmt.Errorf("missing stream: %s", k.String())
	}
	ses.sm.mu.RUnlock()

	cStream, err := str.AttachToStream(ctx)
	if err != nil {
		return err
	}

	slog.Debug("client added a stream watch")

	defer cStream.Close()

	// send the backlog of packets seen before they attached.
	op := str.rb.GetAll()
	slog.Debug("sending packet backlog")
	for _, p := range op {
		outP := &pb.APPacket{
			//			Seq:    p.seq,
			OpCode: uint32(p.OpCode),
			Data:   p.Payload,
		}
		if err := stream.Send(outP); err != nil {
			return err
		}
	}

	slog.Debug("looping new packets")

	// loop sending the packets to the client
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-cStream.handle:
			if !ok {
				return nil
			}

			op := &pb.APPacket{
				Seq:    p.seq,
				OpCode: uint32(p.packet.OpCode),
				Data:   p.packet.Payload,
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
