package eqStruct

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/exp/constraints"
)

type EQType uint32

const (
	EQT_Unknown EQType = iota
	EQT_PlayerProfile
	EQT_PlayEverquestResponse
	EQT_ZoneServerInfo
	EQT_PlayRequest
	EQT_ServerZoneEntry
	EQT_LogServer
	EQT_EnterWorld
	EQT_LoginInfo
	EQT_LoginAccepted
)

type EQStruct interface {
	EQType() EQType
	bp() *int
	Unmarshal(b []byte) error // This is only temporary until the VM is implemented.
}

type EQTypes interface {
	uint8 | uint16 | uint32 | string | []byte
}

func EQRead[T EQTypes](b []byte, s EQStruct, field *T, size int) error {
	switch t := any(field).(type) {
	case *uint32:
		return EQReadUint32(b, s, t)
	case *uint16:
		return EQReadUint16(b, s, t)
	case *uint8:
		return EQReadUint8(b, s, t)
	case *[]byte:
		return EQReadBytes(b, s, t, size)
	case *string:
		if size == 0 {
			return EQReadStringNullTerm(b, s, t)
		}
		return EQReadString(b, s, t, size)
	default:
		return fmt.Errorf("unsupported type %t", t)
	}
}

func EQReadUint32(b []byte, s EQStruct, field *uint32) error {
	p := s.bp()
	c := b[*p:]
	*field = binary.BigEndian.Uint32(c)
	*p += 4

	return nil
}

func EQReadUint16(b []byte, s EQStruct, field *uint16) error {
	p := s.bp()
	c := b[*p:]
	*field = binary.BigEndian.Uint16(c)
	*p += 2

	return nil
}

func EQReadUint8(b []byte, s EQStruct, field *uint8) error {
	p := s.bp()
	c := b[*p]
	*field = uint8(c)
	*p++

	return nil
}

func EQReadBytes(b []byte, s EQStruct, field *[]byte, maxLength int) error {
	p := s.bp()
	bh := make([]byte, maxLength)
	stop := min(len(b)-*p, *p+maxLength+1)
	copy(bh, b[*p:stop])
	*field = bh
	*p += maxLength

	return nil
}

func EQReadString(b []byte, s EQStruct, field *string, maxLength int) error {
	p := s.bp()

	bh := make([]byte, maxLength)
	stop := min(len(b)-*p, *p+maxLength+1)
	copy(bh, b[*p:stop])
	bh = bytes.Trim(bh, "\x00")
	*field = string(bh)
	*p += maxLength

	return nil
}

func EQReadStringNullTerm(b []byte, s EQStruct, field *string) error {
	p := s.bp()

	var str strings.Builder
	for {
		if b[*p] == 0 {
			break
		}
		str.WriteByte(b[*p])
		*p++
	}
	*field = str.String()
	*p++

	return nil
}

func min[T constraints.Ordered](x, y T) T {
	if cmp.Less[T](x, y) {
		return x
	}
	return y
}
