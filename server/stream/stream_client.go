package stream

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

type streamClient struct {
	ID     uuid.UUID
	Handle chan StreamPacket
	info   string
	parent *Stream
}

// Send relays the packet to the attached client session.
func (c *streamClient) Send(ctx context.Context, ap StreamPacket) {
	// TODO: convert this to a ring buffer
	select {
	case c.Handle <- ap:
	case <-ctx.Done():
	default:
		slog.Error("failed to send to client", "client", c, "opcode", ap.Packet.OpCode)
	}
}

func (c *streamClient) String() string {
	return c.info
}

func (sc *streamClient) Close() {
	slog.Info("client disconnecting", "client", sc.info)
	sc.parent.mu.Lock()
	delete(sc.parent.Clients, sc.ID)
	sc.parent.mu.Unlock()

	if sc.Handle == nil {
		return
	}

	handle := sc.Handle
	sc.Handle = nil
	close(handle)
}
