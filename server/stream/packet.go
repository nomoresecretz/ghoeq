package stream

import (
	"time"

	"github.com/nomoresecretz/ghoeq-common/decoder"
	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
	"github.com/nomoresecretz/ghoeq/common/eqOldPacket"
	timepb "google.golang.org/protobuf/types/known/timestamppb"
)

type StreamPacket struct {
	Origin time.Time
	Stream *Stream
	Packet *eqOldPacket.EQApplication
	Seq    uint64
	OpCode decoder.OpCode
	Obj    eqStruct.EQStruct
}

func (s *StreamPacket) Proto() *pb.APPacket {
	return &pb.APPacket{
		Origin:   timepb.New(s.Origin),
		Seq:      s.Seq,
		OpCode:   uint32(s.OpCode),
		Data:     s.Packet.Payload,
		StreamId: s.Stream.ID,
	}
}
