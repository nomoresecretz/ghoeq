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
)

const (
	clientBuffer = 50 // packets
	procBuffer   = 50
)

type sessionHandle interface {
	Close()
}
type session struct {
	id         uuid.UUID
	handle     sessionHandle
	mu         sync.RWMutex
	lastClient time.Time
	source     string
	sm         *streamMgr
	mgr        *sessionMgr
}

func NewSession(id uuid.UUID, src string, sm *sessionMgr) *session {
	return &session{
		id:     id,
		source: src,
		mgr:    sm,
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

	d := decoder.NewDecoder()
	if err := d.LoadMap(*opMap); err != nil {
		return err
	}

	// TODO: replace this with lockless ring buffer.
	apc := make(chan StreamPacket, procBuffer)
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

	return wg.Wait()
}

// Close closes the underlying source, causing a chain of graceful closures up the handler stack, ultimately gracefully ending the session. Non Blocking.
func (s *session) Close() {
	s.handle.Close()
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
		p.stream.gameClient.Run(p)
	} else {
		s.sm.clientWatch.Run(p)
	}

	if p.stream.sType == ST_UNKNOWN {
		slog.Error("unknown packet stream", "stream", p.stream, "opCode", p.packet.OpCode)
	}

	if err := p.stream.FanOut(ctx, p); err != nil {
		return err
	}

	return nil
}
