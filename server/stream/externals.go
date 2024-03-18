package stream

import (
	"context"
	"sync"

	"github.com/nomoresecretz/ghoeq/server/assembler"
	"github.com/nomoresecretz/ghoeq/server/common"
)

type streamMgr interface {
	AddStream(key assembler.Key, s *Stream)
	Decoder() common.OpDecoder
	Close()
	Mu() *sync.RWMutex
	ClientStreams() map[assembler.Key]*Stream
}

type gameClient interface {
	Run(ctx context.Context, p *StreamPacket) error
	DeleteStream(key assembler.Key)
}
