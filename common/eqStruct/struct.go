package eqStruct

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/exp/constraints"
)

type EQType uint32

const (
	EQT_Unknown EQType = iota
	EQT_PlayerProfile
	EQT_PlayEverquestResponse
	EQT_ZoneServerInfo
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
	c := b[*p:]
	*field = binary.BigEndian.Uint16(c)
	*p += 2
	return nil
}

func EQReadString(b []byte, s EQStruct, field *string, maxLength int) error {
	p := s.bp()

	bh := make([]byte, maxLength)
	copy(bh, b[*p:*p+maxLength+1])
	bh = bytes.Trim(bh, "\x00")
	*field = string(bh)
	*p += maxLength
	return nil
}
