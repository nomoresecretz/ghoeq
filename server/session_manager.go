package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nomoresecretz/ghoeq/server/common"
	"github.com/nomoresecretz/ghoeq/server/game_client"
	"golang.org/x/sync/errgroup"
)

const (
	clientTimeout = 10 * time.Minute
)

// var errDuplicateSession = errors.New("duplicate capture session").
type sessionRequest struct {
	replyChan chan<- replyStruct
	src       string
	mode      string
}

type sessionMgr struct {
	muSessions  sync.RWMutex
	ctrlChan    chan<- *sessionRequest
	sessions    map[uuid.UUID]*session
	parent      *ghoeqServer
	clientWatch *game_client.GameClientWatch
}

func NewSessionManager() (*sessionMgr, error) {
	cw, err := game_client.NewClientWatch()
	if err != nil {
		return nil, fmt.Errorf("failed to make clientwatch: %w", err)
	}

	return &sessionMgr{
		sessions:    make(map[uuid.UUID]*session),
		clientWatch: cw,
	}, nil
}

func (s *sessionMgr) genSessionID() uuid.UUID {
	return uuid.New()
}

// Go manages goroutine lifetime for the sessions and message brokers.
func (sm *sessionMgr) Run(ctx context.Context, gs *ghoeqServer, d common.OpDecoder, singleMode bool) error {
	sm.parent = gs

	return sm.requestHandler(ctx, d, singleMode)
}

// requestHandler runs the handler loop to manage capture sessions.
func (s *sessionMgr) requestHandler(ctx context.Context, d common.OpDecoder, singleMode bool) error {
	g, wctx := errgroup.WithContext(ctx)
	sc := make(chan *sessionRequest)
	s.ctrlChan = sc

	var done bool

	for !done {
		select {
		case <-wctx.Done():
			done = true
		case r, ok := <-sc:
			if !ok {
				done = true
				break
			}

			if err := s.handleRequest(wctx, d, r, g); err != nil {
				return err
			}

			if singleMode {
				done = true
			}
		}
	}

	defer s.clientWatch.GracefulStop()

	return g.Wait()
}

func (sm *sessionMgr) handleRequest(ctx context.Context, d common.OpDecoder, r *sessionRequest, g *errgroup.Group) error {
	switch r.mode {
	case "Start":
		// TODO: add duplicate checking
		g.Go(func() error {
			return sm.runCapture(ctx, d, r.src, r.replyChan) // TODO: test capture first so we can return error to client instead of killing server. :)
		})

		return nil
	case "Stop":
		r.replyChan <- replyStruct{
			err: fmt.Errorf("unimplemented"),
		}
		close(r.replyChan)
	}

	return fmt.Errorf("unimplemented")
}

// runCapture does the actual work of starting a capture session and holding the work goroutines.
func (sm *sessionMgr) runCapture(ctx context.Context, d common.OpDecoder, src string, c chan<- replyStruct) error {
	sm.muSessions.Lock()
	index := sm.genSessionID()
	s := NewSession(index, src, sm)
	sm.sessions[index] = s
	sm.muSessions.Unlock()

	defer sm.cleanSession(index)
	c <- replyStruct{
		reply: index,
	}
	close(c)
	slog.Info("starting capture", "source", src)

	return s.Run(ctx, src, d)
}

// cleanSession removes the capture session from tracking and any cleanup.
func (s *sessionMgr) cleanSession(i uuid.UUID) {
	s.muSessions.Lock()
	ses := s.sessions[i]
	delete(s.sessions, i)
	ses.Close()
	s.muSessions.Unlock()
}

func (sm *sessionMgr) GracefulStop() {
	sm.muSessions.RLock()
	defer sm.muSessions.RUnlock()

	for _, ses := range sm.sessions {
		ses.Close()
	}
	sm.clientWatch.GracefulStop()
}

func (sm *sessionMgr) SessionById(sId uuid.UUID) (*session, error) {
	sm.muSessions.RLock()

	ses, ok := sm.sessions[sId]
	if !ok {
		sm.muSessions.RUnlock()
		return nil, fmt.Errorf("unknown session: %s", sId.String())
	}
	sm.muSessions.RUnlock()

	return ses, nil
}
