package stream

import (
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
	Run(*StreamPacket) error
	DeleteStream(assembler.Key)
}
