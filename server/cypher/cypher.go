package cypher

import (
	"bytes"
	"encoding/binary"
	"io"
)

type CodePoint interface {
	rune | byte | int
}

func bitxor[V CodePoint](a, b V) V {
	return a ^ b
}

type cypherbox struct {
	i  uint32
	b  []uint64
	c  uint64
	cy Cypher
}

type Cypher interface {
	Walk(*cypherbox)
	WalkInvert(*cypherbox)
	Flip(*cypherbox)
	Seed(*cypherbox)
}

//Walk runs a single step in the encryption process.
func (c *cypherbox) Walk() {
	c.cy.Walk(c)
}

//WalkInvert reverses a single step in the cbox encryption process.
func (c *cypherbox) WalkInvert() {
	c.cy.WalkInvert(c)
}

//Flip is a CBox function to handle the single pass pre encoding box swaps.
func (c *cypherbox) Flip() {
	c.cy.Flip(c)
}

//Seed is intended to be a cbox handler INIT function if required.
func (c *cypherbox) Seed() {
	c.cy.Seed(c)
}

func (c *cypherbox) Fill(d []byte) {
	ll := len(d)
	mod := ll % 8
	if mod > 0 {
		final := make([]byte, 8-mod)
		final = append(final, d[ll-mod:]...)
		d = append(d[:ll-mod], final...)
		c.b = append(c.b, 0)
	}
	for i := 0; i < len(c.b); i++ {
		iv := i << 3
		c.b[i] = binary.LittleEndian.Uint64(d[iv:])
	}
}

//Dump returns the contents of the CBox, presumably after processing.
func (c *cypherbox) Dump(skip int) []byte {
	ll := len(c.b)
	b := []byte{}
	bt := make([]byte, 8)
	for i := 0; i < ll; i++ {
		binary.LittleEndian.PutUint64(bt, c.b[i])
		b = append(b, bt...)
	}
	return b[skip:]
}

//NewCBox returns a CypherBox designed to decrypt various flavors of client packets. Has Pluggable modules.
func NewCBox(s int, c Cypher) *cypherbox {
	size := s >> 3
	b := make([]uint64, int(size))

	return &cypherbox{
		b:  b,
		cy: c,
	}
}

//NewReader returns an io.readerized version of the basic Dump function.
func (c *cypherbox) NewReader(skip int) io.Reader {
	return bytes.NewReader(c.Dump(skip))
}

func (c *cypherbox) Len() int {
	return len(c.b)
}

type cypherNull struct{}

func (c *cypherNull) Walk(*cypherbox)       {}
func (c *cypherNull) WalkInvert(*cypherbox) {}
func (c *cypherNull) Flip(*cypherbox)       {}
func (c *cypherNull) Seed(*cypherbox)       {}

//NewNull returns a non decryption handler, basically intended to handle the compressed but unencrypted packets.
func NewNull() *cypherNull {
	return &cypherNull{}
}

func (c *cypherbox) Roll(chunk uint64, bit int) uint64 {
	return ((chunk << bit) | (chunk >> (64 - bit)))
}

func (c *cypherbox) URoll(chunk uint64, bit int) uint64 {
	return ((chunk >> bit) | (chunk << (64 - bit)))
}
