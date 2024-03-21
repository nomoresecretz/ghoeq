package common

import (
	"github.com/nomoresecretz/ghoeq-common/decoder"
)

type OpDecoder interface {
	GetOp(opCode decoder.OpCode) string
	GetOpByName(name string) decoder.OpCode
}

const ClientBuffer = 50 // packets
