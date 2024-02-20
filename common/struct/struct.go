package eqstruct

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/exp/constraints"
)

type EQType uint32

const (
	EQT_Unknown EQType = iota
	EQT_PlayerProfile
	EQT_PlayEverquestResponse
)

type EQStruct interface {
	EQType() EQType
	bp() *int
}

type EQTypes interface {
	constraints.Integer
}

func EQRead(b []byte, s EQStruct, field any, size int) error {
	switch t := field.(type) {
	case *uint16:
		return EQReadUint16(b, s, t)
	default:
		return fmt.Errorf("unsupported type %t", t)
	}
}

func EQReadUint16(b []byte, s EQStruct, field *uint16) error {
	p := s.bp()
	c := b[*p : *p+16+1]
	*field = binary.BigEndian.Uint16(c)
	*p += 4
	return nil
}
