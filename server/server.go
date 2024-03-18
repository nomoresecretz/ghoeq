package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	structPb "github.com/nomoresecretz/ghoeq-common/proto/eqstruct"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
	"github.com/nomoresecretz/ghoeq/server/common"
	"github.com/nomoresecretz/ghoeq/server/stream"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	timepb "google.golang.org/protobuf/types/known/timestamppb"
)

type ghoeqServer struct {
	allowedDev map[string]struct{}

	sMgr *sessionMgr
	pb.UnimplementedBackendServerServer
	debugFlag bool
}

func New(ctx context.Context, cDev []string) (*ghoeqServer, error) {
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

func (s *ghoeqServer) Run(ctx context.Context, d common.OpDecoder, singleMode bool) error {
	sm, err := NewSessionManager()
	if err != nil {
		return err
	}

	s.sMgr = sm
	return s.sMgr.Run(ctx, s, d, singleMode)
}

func (s *ghoeqServer) ListSources(ctx context.Context, r *pb.ListRequest) (*pb.ListSourcesResponse, error) {
	sl, err := GetSources()
	if err != nil {
		return nil, err
	}

	var rs []*pb.Source

	seen := make(map[string]struct{})

	l := len(s.allowedDev)

	for _, i := range sl {
		seen[i.Name] = struct{}{}

		if !s.validSource(i.Name) && l > 0 {
			continue
		}
		// TODO: consider adding address info
		rs = append(rs, &pb.Source{
			Id:          i.Name,
			Description: i.Description,
		})
	}

	for s := range s.allowedDev {
		if _, ok := seen[s]; !ok && strings.HasPrefix(s, "file://") {
			rs = append(rs, &pb.Source{
				Id:          s,
				Description: s,
			})
		}
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
		if streamId != "" && streamId != str.Key.String() {
			continue
		}
		streams = append(streams, str.Proto())
	}
	sess.mu.RUnlock()

	return &pb.ListStreamsResponse{Streams: streams}, nil
}

func (s *ghoeqServer) ModifySession(ctx context.Context, r *pb.ModifySessionRequest) (*pb.ModifySessionResponse, error) {
	rpl := []*pb.SessionResponse{}

	for _, m := range r.Mods {
		r, err := s.handleSessionRequest(m)
		if err != nil {
			return nil, err
		}
		rpl = append(rpl, &pb.SessionResponse{
			Id:    r,
			State: m.State,
		})
	}

	return &pb.ModifySessionResponse{
		Responses: rpl,
	}, nil
}

func (s *ghoeqServer) AttachClientStream(r *pb.AttachClientStreamRequest, cStream pb.BackendServer_AttachClientStreamServer) error {
	ctx := cStream.Context()
	rid := r.ClientId

	cli, err := s.sMgr.clientWatch.WaitForClient(ctx, r.GetClientId())
	if err != nil {
		return fmt.Errorf("failed to get game client: %w", err)
	}

	cs, err := cli.AttachToStream(ctx)
	if err != nil {
		return fmt.Errorf("failed attaching to game client stream: %w", err)
	}

	handle := cs.Handle

	switch rid {
	case "":
		var cupd *eqStruct.ClientUpdate
		if r.GetClientId() == "" {
			cupd = &eqStruct.ClientUpdate{
				ClientID: string(cli.ID.String()),
				// Address: cli., // TODO: add client address logic.
			}
		}

		eqstr, err := protoToMsg(cupd)
		if err != nil {
			slog.Error("message failure: ", "error", err, "packet", spew.Sdump(cupd))
		}

		r1 := &pb.ClientPacket{
			Struct: eqstr,
		}

		if err := cStream.Send(r1); err != nil {
			return err
		}
	}

	sf := func(p stream.StreamPacket) error {
		cp, err := s.makeOutStructPacket(p)
		if err != nil {
			return err
		}

		return cStream.Send(cp)
	}

	if err := cli.Historic(ctx, sf, r.LastUpdate.AsTime()); err != nil {
		return fmt.Errorf("failed sending historic state: %w", err)
	}

	slog.Debug("looping new packets")

	// signal the history dump is complete.
	if err := cStream.Send(&pb.ClientPacket{
		StreamId: "break",
	}); err != nil {
		return err
	}

	return s.sendLoopStruct(ctx, handle, cStream)
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
			Id: string(cli.ID.String()),
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
	cli.Mu.RLock()
	p = cli.Ping

	var strz []*pb.Stream
	for _, v := range cli.Streams {
		streamProto := v.Proto()
		streamProto.Session = v.SF.Mgr().(*streamMgr).session.Proto()
		strz = append(strz, streamProto)
		seen[v.ID] = struct{}{}
	}
	cli.Mu.RUnlock()

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
			if p == nil {
				return nil
			}

			var strz []*pb.Stream

			for _, v := range cli.Streams {
				streamProto := v.Proto()
				if _, ok := seen[v.ID]; ok {
					continue
				}
				slog.Debug("notifying watcher of new gameClient stream")
				streamProto.Session = v.SF.Mgr().(*streamMgr).session.Proto()
				strz = append(strz, streamProto)
				seen[v.ID] = struct{}{}
			}

			if err := stream.Send(&pb.ClientUpdate{Streams: strz}); err != nil {
				return err
			}
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
	op := str.RB.GetAll()
	slog.Debug("sending packet backlog")

	seen := make(map[uint64]struct{})
	for _, p := range op {
		seen[p.Seq] = struct{}{}
		outP := p.Proto()
		if err := stream.Send(outP); err != nil {
			return err
		}
	}

	slog.Debug("looping new packets")

	return s.sendLoopRaw(ctx, cStream.Handle, stream, seen)
}

type eqProto interface {
	ProtoMess() proto.Message
}

// AttachStreamRaw provides a single full client stream of decrypted but unprocessed EQApplication packets.
func (s *ghoeqServer) AttachStreamStruct(r *pb.AttachStreamRequest, stream pb.BackendServer_AttachStreamStructServer) error {
	ctx := stream.Context()

	ses, err := s.getSession(r.GetSessionId())
	if err != nil {
		return fmt.Errorf("unable to find session: %w", err)
	}

	str, err := ses.sm.StreamById(r.GetId())
	if err != nil {
		return fmt.Errorf("unable to get stream by id: %w", err)
	}

	cStream, err := str.AttachToStream(ctx)
	if err != nil {
		return fmt.Errorf("failed to attach to stream: %w", err)
	}

	slog.Debug("client added a stream watch")

	defer cStream.Close()

	// send the backlog of packets seen before they attached.
	op := str.RB.GetAll()
	slog.Debug("sending packet backlog")

	seen := make(map[uint64]struct{})
	for _, p := range op {
		seen[p.Seq] = struct{}{}

		outP, err := s.makeOutStructPacket(p)
		if err != nil {
			return fmt.Errorf("failed to make struct packet: %w", err)
		}

		if err := stream.Send(outP); err != nil {
			return err
		}
	}

	slog.Debug("looping new packets")

	return s.sendLoopStruct(ctx, cStream.Handle, stream)
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

	return s.sendLoopRaw(ctx, cStream.handle, stream, nil)
}

type streamSender interface {
	Send(*pb.APPacket) error
	grpc.ServerStream
}

type streamSenderStruct interface {
	Send(*pb.ClientPacket) error
	grpc.ServerStream
}

func (s *ghoeqServer) sendLoopRaw(ctx context.Context, handle <-chan stream.StreamPacket, stream streamSender, seen map[uint64]struct{}) error {
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
				if _, ok := seen[p.Seq]; ok {
					continue
				}
				seen[p.Seq] = struct{}{}
				delete(seen, p.Seq-100)
			}

			op := p.Proto()

			if err := stream.Send(op); err != nil {
				return err
			}
		}
	}
}

func (s *ghoeqServer) sendLoopStruct(ctx context.Context, handle <-chan stream.StreamPacket, stream streamSenderStruct) error {
	// loop sending the packets to the client
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-handle:
			if !ok {
				return nil
			}

			outP, err := s.makeOutStructPacket(p)
			if err != nil {
				return err
			}

			if err := stream.Send(outP); err != nil {
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

func (s *ghoeqServer) makeOutStructPacket(p stream.StreamPacket) (*pb.ClientPacket, error) {
	eqstr, err := getMsg(p)
	if err != nil {
		slog.Error("message failure: ", "error", err, "packet", spew.Sdump(p))
	}

	outP := &pb.ClientPacket{
		Origin: timepb.New(p.Origin),
		Seq:    p.Seq,
		OpCode: uint32(p.OpCode),
		Struct: eqstr,
	}

	if s.debugFlag {
		outP.Data = p.Packet.Payload
	}

	if eqstr == nil {
		outP.Data = p.Packet.Payload
	}

	return outP, nil
}

func getMsg(p stream.StreamPacket) (*structPb.DataStruct, error) {
	if p.Obj == nil && p.Packet.OpCode != 0x5f41 {
		return nil, nil
	}

	obj, ok := p.Obj.(eqProto)
	if !ok {
		return nil, nil
	}

	return protoToMsg(obj)
}

func protoToMsg(p eqProto) (*structPb.DataStruct, error) {
	msg := p.ProtoMess()
	if msg == nil {
		return nil, nil
	}

	anyMsg, err := anypb.New(msg)
	if err != nil {
		return nil, fmt.Errorf("anymsg conversion err: %w", err)
	}

	return &structPb.DataStruct{
		// Type: structPb.StructType(p.Obj.EQType()), // deprecated, to be removed
		Msg: anyMsg,
	}, nil
}
