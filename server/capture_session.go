package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq-common/decoder"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/peer"

	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
)

const (
	clientBuffer = 50 // packets
	procBuffer   = 50
)

type sessionHandle interface {
	Close()
}
type session struct {
	id     uuid.UUID
	handle sessionHandle

	mu         sync.RWMutex
	lastClient time.Time
	source     string
	sm         *streamMgr
	mgr        *sessionMgr
	clients    map[uuid.UUID]*sessionClient
	clientChan chan<- StreamPacket
	onceClose  sync.Once
}

type sessionClient struct {
	id     uuid.UUID
	handle chan StreamPacket
	info   string
	parent *session
}

func NewSession(id uuid.UUID, src string, sm *sessionMgr) *session {
	return &session{
		id:      id,
		source:  src,
		mgr:     sm,
		clients: make(map[uuid.UUID]*sessionClient),
	}
}

// Run does the actual capture session work, holding all goroutines for the lifetime of the capture session.
func (s *session) Run(ctx context.Context, src string, d opDecoder) error {
	h, err := getHandle(src)
	if err != nil {
		return fmt.Errorf("failed to get capture handle: %w", err)
	}

	s.handle = h

	defer h.Close()

	// TODO: replace this with lockless ring buffer.
	apc := make(chan StreamPacket, procBuffer)
	apcb := make(chan StreamPacket, procBuffer)
	s.clientChan = apcb
	sm := NewStreamMgr(d, s.mgr.clientWatch)
	s.sm = sm
	sm.session = s

	wg, wctx := errgroup.WithContext(ctx)
	/*  TODO: prune unneeded capture sessions.
	wg.Go(func() error {
		s.timeoutWatch(wctx)

		return nil
	})
	*/
	wg.Go(func() error {
		return sm.NewCapture(wctx, h, apc, wg)
	})
	wg.Go(func() error {
		defer sm.Close()

		return s.processPackets(wctx, apc)
	})
	wg.Go(func() error {
		return s.handleClients(wctx, apcb)
	})

	return wg.Wait()
}

// Close closes the underlying source, causing a chain of graceful closures up the handler stack, ultimately gracefully ending the session. Non Blocking.
func (s *session) Close() {
	s.handle.Close()
	s.onceClose.Do(func() {
		if s.clientChan == nil {
			s.closeClients()
			return
		}
		close(s.clientChan)
	})
}

/*
// timeoutWatch autocloses the session if there are no clients after a given period.
func (s *session) timeoutWatch(ctx context.Context) {
	tik := time.NewTimer(clientTimeout)
	defer func() {
		tik.Stop()
	}()

	newTimer := func() {
		remain := time.Until(s.lastClient.Add(clientTimeout))
		tik = time.NewTimer(remain)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tik.C:
			s.mu.RLock()
			if len(s.clients) > 0 {
				s.lastClient = time.Now()

				newTimer()
				s.mu.RUnlock()

				continue
			}

			if time.Since(s.lastClient) >= clientTimeout {
				s.Close()
				s.mu.RUnlock()

				return
			}

			newTimer()
			s.mu.RUnlock()
		}
	}
}
*/

// processPackets is just a placeholder for the grunt work until the real structure exists.
func (s *session) processPackets(ctx context.Context, cin <-chan StreamPacket) error {
	c := NewCrypter()

	for p := range cin {
		if err := s.processPacket(ctx, p, c); err != nil {
			return err
		}
	}

	return nil
}

func (s *session) processPacket(ctx context.Context, p StreamPacket, c *crypter) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ap := p.packet

	d := s.sm.decoder

	opCode := d.GetOp(decoder.OpCode(ap.OpCode))
	if opCode == "" {
		opCode = fmt.Sprintf("%#4x", ap.OpCode)
	}

	if c.IsCrypted(opCode) {
		res, err := c.Decrypt(opCode, ap.Payload)
		if err != nil {
			slog.Error(fmt.Sprintf("error decrpyting %s", err))
		}

		ap.Payload = res
	}

	if p.stream.gameClient != nil {
		p.stream.gameClient.Run(&p)
	} else {
		s.sm.clientWatch.Run(&p)
	}

	p.stream.rb.Add(p)

	if err := s.clientSend(ctx, p); err != nil {
		return err
	}

	if err := p.stream.FanOut(ctx, p); err != nil {
		return err
	}

	return nil
}

// handleClients runs the fanout loop for client sessions.
func (s *session) handleClients(ctx context.Context, apcb <-chan StreamPacket) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-apcb:
			if !ok {
				s.closeClients()

				return nil // chan closure
			}

			s.mu.RLock()
			for _, c := range s.clients {
				c.Send(ctx, p)
			}
			s.mu.RUnlock()
		}
	}
}

func (s *session) closeClients() {
	s.mu.RLock()
	for _, sc := range s.clients {
		if sc.handle == nil {
			continue
		}

		close(sc.handle)
		sc.handle = nil
	}
	s.mu.RUnlock()
}

// Send relays the packet to the attached client session.
func (c *sessionClient) Send(ctx context.Context, p StreamPacket) {
	// TODO: convert this to a ring buffer
	select {
	case c.handle <- p:
	case <-ctx.Done():
	default:
		slog.Error("failed to send to client", "client", c, "opcode", p.opCode)
	}
}

func (s *session) clientSend(ctx context.Context, p StreamPacket) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.clientChan <- p:
	default:
		return fmt.Errorf("failed to send to client handler: %s", s.id)
	}
	return nil
}

func (c *sessionClient) String() string {
	return c.info
}

func (s *session) AddClient(ctx context.Context) (*sessionClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan StreamPacket, clientBuffer)
	cinfo, ok := peer.FromContext(ctx)

	var clientTag string
	if ok {
		clientTag = cinfo.Addr.String()
	}

	slog.Info("capture session adding client", "session", s.id.String(), "client", clientTag)

	id := uuid.New()
	c := &sessionClient{
		handle: ch,
		info:   clientTag,
		parent: s,
		id:     id,
	}
	s.clients[id] = c

	return c, nil
}

func (sc *sessionClient) Close() {
	slog.Info("client disconnecting", "client", sc.info)
	sc.parent.mu.Lock()
	delete(sc.parent.clients, sc.id)
	sc.parent.mu.Unlock()

	if sc.handle == nil {
		return
	}

	close(sc.handle)
	sc.handle = nil
}

func (s *session) Proto() *pb.Session {
	return &pb.Session{
		Id:     s.id.String(),
		Source: s.source,
	}
}
