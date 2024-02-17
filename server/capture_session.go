package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq/common/decoder"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/peer"
)

const (
	clientBuffer = 50 // packets
	fanBuffer    = 10
)

type sessionHandle interface {
	Close()
}

type sessionClient struct {
	id     uuid.UUID
	handle chan *EQApplication
	info   string
	parent *session
}

type session struct {
	id         uuid.UUID
	handle     sessionHandle
	mu         sync.RWMutex
	clients    map[uuid.UUID]*sessionClient
	lastClient time.Time
	source     string
}

func NewSession(id uuid.UUID, src string) *session {
	return &session{
		id:      id,
		source:  src,
		clients: make(map[uuid.UUID]*sessionClient),
	}
}

// Run does the actual capture session work, holding all goroutines for the lifetime of the capture session.
func (s *session) Run(ctx context.Context, src string) error {
	h, err := liveHandle(src) // TODO: Support other kinds of captures here.
	if err != nil {
		return err
	}
	s.handle = h

	defer h.Close()

	// TODO: replace this with lockless ring buffer.
	apc := make(chan *EQApplication, fanBuffer)
	apcb := make(chan *EQApplication, fanBuffer)
	sm := NewStreamMgr()

	wg, wctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		s.timeoutWatch(wctx)

		return nil
	})
	wg.Go(func() error {
		return sm.NewCapture(wctx, h, apc)
	})
	wg.Go(func() error {
		defer close(apcb)

		return s.processPackets(wctx, apc, apcb, sm)
	})
	wg.Go(func() error {
		return s.handleClients(wctx, apcb, sm)
	})

	return wg.Wait()
}

// Close closes the underlying source, causing a chain of graceful closures up the handler stack, ultimately gracefully ending the session. Non Blocking.
func (s *session) Close() {
	s.handle.Close()
}

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

// processPackets is just a placeholder for the grunt work until the real structure exists.
func (s *session) processPackets(ctx context.Context, cin <-chan *EQApplication, cout chan<- *EQApplication, sm *streamMgr) error {
	c := NewCrypter()
	d := decoder.NewDecoder()
	if err := d.LoadMap(*opMap); err != nil {
		return err
	}

	for p := range cin {
		if err := ctx.Err(); err != nil {
			return err
		}
		opCode := d.GetOp(p.OpCode)
		if opCode == "" {
			opCode = fmt.Sprintf("%#4x", p.OpCode)
		}

		if c.IsCrypted(opCode) {
			res, err := c.Decrypt(opCode, p.Payload)
			if err != nil {
				slog.Error(fmt.Sprintf("error decrpyting %s", err))
			}
			p.Payload = res
		}
		cout <- p
	}

	return nil
}

// handleClients runs the fanout loop for client sessions.
func (s *session) handleClients(ctx context.Context, apcb <-chan *EQApplication, sm *streamMgr) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p, ok := <-apcb:
			if !ok {
				s.mu.RLock()
				for _, sc := range s.clients {
					if sc.handle == nil {
						continue
					}

					close(sc.handle)
					sc.handle = nil
				}
				s.mu.RUnlock()

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

// Send relays the packet to the attached client session.
func (c *sessionClient) Send(ctx context.Context, p *EQApplication) {
	// TODO: convert this to a ring buffer
	select {
	case c.handle <- p:
	case <-ctx.Done():
	default:
		slog.Error("failed to send to client", "client", c, "opcode", p.OpCode)
	}
}

func (c *sessionClient) String() string {
	return c.info
}

func (s *session) AddClient(ctx context.Context) (*sessionClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan *EQApplication, clientBuffer)
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
