package main

import (
	"compress/zlib"
	"errors"
	"fmt"
	"io"

	"github.com/nomoresecretz/ghoeq/server/cypher"
)

var ErrNoCypher = errors.New("unknown cyphertype")

// TODO: convert to registration based system

type crypter struct {
	opMap map[string]cypher.Cypher
}

// NewCrypter returns an object capable of decoding non clear data packets.
func NewCrypter() *crypter {
	return &crypter{
		opMap: map[string]cypher.Cypher{
			"OP_CharInventory":       cypher.NewNull(),
			"OP_ShopInventoryPacket": cypher.NewNull(),
		},
	}
}

func (c *crypter) IsCrypted(s string) bool {
	_, err := c.getCypher(s)
	
	return err == nil
}

func (c *crypter) getCypher(s string) (cypher.Cypher, error) {
	cy, ok := c.opMap[s]
	if !ok {
		return nil, fmt.Errorf("%s : %w", s, ErrNoCypher)
	}
	return cy, nil
}

// Decrypt does the work of decrypting a EQAppPacket. Currently only supports Quarm style.
func (c *crypter) Decrypt(s string, data []byte) ([]byte, error) {
	skipHeader := 0
	if s == "OP_CharInventory" || s == "OP_ShopInventoryPacket" {
		skipHeader = 2
	}
	cy, err := c.getCypher(s)
	if err != nil {
		return nil, err
	}
	la := len(data) - skipHeader
	cb := cypher.NewCBox(la, cy)
	cb.Seed()
	cb.Fill(data[skipHeader:])
	for k := 0; k < cb.Len(); k++ {
		cb.WalkInvert()
	}
	cb.Flip()
	z, err := zlib.NewReader(cb.NewReader(0))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return io.ReadAll(z)
}
