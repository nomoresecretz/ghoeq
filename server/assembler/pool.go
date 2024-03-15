package assembler

import (
	"context"
	"sync"

	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
)

const initialAllocSize = 1024

// TODO: maybe a memory pool? not sure its worth the trouble.
type streamPool struct {
	mu        sync.RWMutex
	users     int
	factory   streamFactory
	conns     map[Key]*connection
	closeChan chan struct{}
}

// NewStreamPool is intended to be a pool for tracking the various known streams, this is just a blind shell to be implemented later.
func NewStreamPool(f streamFactory) *streamPool {
	return &streamPool{
		conns:     make(map[Key]*connection),
		factory:   f,
		closeChan: make(chan struct{}),
	}
}

func (p *streamPool) remove(conn *connection) {
	p.mu.Lock()
	delete(p.conns, conn.key)
	p.mu.Unlock()
}

func (p *streamPool) getConn(k Key) *connection {
	conn := p.conns[k]
	if conn != nil {
		return conn
	}

	return nil
}

func (p *streamPool) Close() {
	close(p.closeChan)
}

// getConnection returns a connection.  If end is true and a connection
// does not already exist, returns nil.  This allows us to check for a
// connection without actually creating one if it doesn't already exist.
func (p *streamPool) getConnection(ctx context.Context, k Key, end bool, op *eqOldPacket.OldEQOuter, ac AssemblerContext) *connection {
	p.mu.RLock()
	conn := p.getConn(k)
	p.mu.RUnlock()

	if end || conn != nil {
		return conn
	}

	s := p.factory.New(ctx, k[0], k[1], op, ac)
	p.mu.Lock()
	defer p.mu.Unlock()
	conn = p.newConnection(ctx, k, s)

	p.conns[k] = conn

	return conn
}
