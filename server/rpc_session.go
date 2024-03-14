package server

import (
	"fmt"

	"github.com/google/uuid"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
)

func (s *ghoeqServer) handleSessionRequest(r *pb.ModifyRequest) error {
	switch r.GetState() {
	case pb.State_STATE_START:
		_, err := s.handleSessionStartRequest(r)
		if err != nil {
			return err
		}
	case pb.State_STATE_STOP:
		return fmt.Errorf("unimplemented")
	case pb.State_STATE_UNKNOWN:
		return fmt.Errorf("unknown state requested")
	}

	return nil
}

func (s *ghoeqServer) handleSessionStartRequest(r *pb.ModifyRequest) (string, error) {
	src := r.GetSource()
	if src == "" || !s.validSource(src) {
		return "", fmt.Errorf("a valid source is required")
	}

	return s.startCapture(src)
}

type replyStruct struct {
	reply any
	err   error
}

func (s *ghoeqServer) startCapture(src string) (string, error) {
	rc := make(chan replyStruct)
	s.sMgr.ctrlChan <- &sessionRequest{
		replyChan: rc,
		src:       src,
		mode:      "Start",
	}

	var reply string

	for r := range rc {
		if r.err != nil {
			return "", r.err
		}

		u, ok := r.reply.(uuid.UUID)
		if !ok {
			return "", fmt.Errorf("invalid reply: %v", reply)
		}

		reply = u.String()
	}

	return reply, nil
}
