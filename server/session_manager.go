package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
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
	clientWatch *gameClientWatch
}

func NewSessionManager() *sessionMgr {
	return &sessionMgr{
		sessions:    make(map[uuid.UUID]*session),
		clientWatch: NewClientWatch(),
	}
}

func (s *sessionMgr) genSessionID() uuid.UUID {
	return uuid.New()
}

// Go manages goroutine lifetime for the sessions and message brokers.
func (sm *sessionMgr) Go(ctx context.Context, gs *ghoeqServer) error {
	sm.parent = gs

	g, wctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return sm.requestHandler(wctx)
	})

	return g.Wait()
}

// requestHandler runs the handler loop to manage capture sessions.
func (s *sessionMgr) requestHandler(ctx context.Context) error {
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
				break
			}

			if err := s.handleRequest(wctx, r, g); err != nil {
				return err
			}
		}
	}

	return g.Wait()
}

func (sm *sessionMgr) handleRequest(ctx context.Context, r *sessionRequest, g *errgroup.Group) error {
	switch r.mode {
	case "Start":
		// TODO: add duplicate checking
		g.Go(func() error {
			return sm.runCapture(ctx, r.src, r.replyChan) // TODO: test capture first so we can return error to client instead of killing server. :)
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
func (sm *sessionMgr) runCapture(ctx context.Context, src string, c chan<- replyStruct) error {
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

	return s.Run(ctx, src)
}

// cleanSession removes the capture session from tracking and any cleanup.
func (s *sessionMgr) cleanSession(i uuid.UUID) {
	s.muSessions.Lock()
	delete(s.sessions, i)
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