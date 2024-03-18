package stream

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

type clientParent interface {
	DeleteClient(id uuid.UUID)
}

type StreamClient struct {
	ID     uuid.UUID
	Handle chan StreamPacket
	info   string
	Parent clientParent
}

// Send relays the packet to the attached client session.
func (c *StreamClient) Send(ctx context.Context, ap StreamPacket) {
	select {
	case c.Handle <- ap:
	case <-ctx.Done():
	default:
		slog.Error("failed to send to client", "client", c, "opcode", ap.Packet.OpCode)
	}
}

func (c *StreamClient) String() string {
	return c.info
}

func (sc *StreamClient) Close() {
	slog.Info("client disconnecting", "client", sc.info)

	sc.Parent.DeleteClient(sc.ID)

	if sc.Handle == nil {
		return
	}

	handle := sc.Handle
	sc.Handle = nil

	close(handle)
}
