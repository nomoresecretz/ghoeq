package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/davecgh/go-spew/spew"
)

var uninteresting = map[string]struct{}{
	"OP_ChecksumSpell": {},
	"OP_ChecksumExe":   {},
	"OP_GuildsList":    {},
}

var interesting = map[string]struct{}{
	"OP_NewZone":       {},
	"OP_PlayerProfile": {},
	"OP_CharInventory": {},
	"OP_ZoneSpawns":    {},
	"OP_SpawnDoor":     {},
	"OP_GroundSpawn":   {},
	"OP_ExpUpdate":     {},
	"OP_HPUpdate":      {},
	"OP_ManaUpdate":    {},
	"OP_Death":         {},
	"OP_DeleteSpawn":   {},
}

// processPackets is just a placeholder for the grunt work until the real structure exists.
func processPackets(ctx context.Context, cin <-chan *EQApplication, s *streamMgr) error {
	c := NewCrypter()
	d := NewDecoder()
	if err := d.LoadMap(*opMap); err != nil {
		return err
	}

	for p := range cin {
		if err := ctx.Err(); err != nil {
			return err
		}
		opCode := d.GetOp(p.OpCode)
		if opCode == "" {
			opCode = fmt.Sprintf("%#4x", p.OpCode)
		}
		if c.IsCrypted(opCode) {
			res, err := c.Decrypt(opCode, p.Payload)
			if err != nil {
				slog.Error(fmt.Sprintf("error decrpyting %s", err))
			}
			p.Payload = res
		}
		if _, exist := uninteresting[opCode]; exist {
			continue
		}
		fmt.Printf("Packet %#4x : OpCode %s %s", s.eqpackets, opCode, spew.Sdump(p.Payload))
		//			}
	}
	return nil
}
