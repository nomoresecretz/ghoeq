package server

import (
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"log/slog"

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
			"OP_SpawnDoor":           cypher.NewNull(),
			"OP_PlayerProfile":       cypher.NewProfile(),
			"OP_ZoneSpawns":          cypher.NewSpawn(),
			"OP_NewSpawn":            cypher.NewSpawn(),
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
	if s == "OP_CharInventory" || s == "OP_ShopInventoryPacket" || s == "OP_SpawnDoor" {
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
	cb.Flip(0)

	// terrible hack to fix a bug.
	r := cb.Peek(0, 1)[0] & 0xffff
	if r != 0x5e78 {
		cb.Flip(0)
		cb.Flip(1)
	}

	z, err := zlib.NewReader(cb.NewReadCloser(0))
	if err != nil {
		return nil, fmt.Errorf("error decrypting (%s): %w", s, err)
	}

	defer z.Close()

	b, err := io.ReadAll(z)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		slog.Error(fmt.Sprintf("error decrpyting %s", err))
	}

	return b, nil
}
