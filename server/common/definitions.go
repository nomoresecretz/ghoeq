package common

import (
	"github.com/nomoresecretz/ghoeq-common/decoder"
)

type OpDecoder interface {
	GetOp(decoder.OpCode) string
	GetOpByName(string) decoder.OpCode
}

const ClientBuffer = 50 // packets
