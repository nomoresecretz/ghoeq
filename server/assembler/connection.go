package assembler

import (
	"context"
	"sync"
	"time"
)

const streamTimeout = 10 * time.Minute

type (
	connection struct {
		mu                sync.RWMutex
		key               Key
		dir               FlowDirection
		frags             map[uint16]*fragItem
		nextSeq           Sequence
		ackSeq            Sequence
		created, lastSeen time.Time
		stream            Stream
		closed            bool
	}
	fragment struct {
		payload []byte
	}
	fragItem struct {
		origin time.Time
		seen   uint16
		frags  []fragment
	}
)

type FlowDirection uint

const (
	DirUnknown FlowDirection = iota
	DirClientToServer
	DirServerToClient
)

func (dir FlowDirection) String() string {
	switch dir {
	case DirClientToServer:
		return "client->server"
	case DirServerToClient:
		return "server->client"
	default:
		return "unknown"
	}
}

// Reverse returns the reversed direction.
func (dir FlowDirection) Reverse() FlowDirection {
	switch dir {
	case DirClientToServer:
		return DirServerToClient
	case DirServerToClient:
		return DirClientToServer
	}

	return dir
}

func (p *streamPool) newConnection(ctx context.Context, k Key, s Stream) (c *connection) {
	c = &connection{
		frags:  make(map[uint16]*fragItem),
		stream: s,
	}
	go p.cleaner(ctx, c)
	p.conns[k] = c

	return c
}

// cleaner prunes timed out connections.
func (s *streamPool) cleaner(ctx context.Context, c *connection) {
	t := time.After(streamTimeout)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closeChan:
			return
		case <-t:
			var ret bool

			t, ret = s.clean(c, &t)
			if ret {
				return
			}
		}
	}
}

func (s *streamPool) clean(c *connection, t *<-chan time.Time) (<-chan time.Time, bool) {
	c.mu.RLock()
	ls := c.lastSeen
	c.mu.RUnlock()

	if time.Since(ls) > streamTimeout {
		s.mu.Lock()
		delete(s.conns, c.key)
		s.mu.Unlock()
		c.Clean()

		return nil, true
	}

	c.mu.RLock()
	remain := time.Since(c.lastSeen)
	c.mu.RUnlock()

	*t = time.After(streamTimeout - remain)

	return *t, false
}

func (c *connection) Clean() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	stream := c.stream
	c.stream = nil

	stream.Clean()
}
