package game_client

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-memdb"
	"github.com/nomoresecretz/ghoeq-common/eqStruct"
	"github.com/nomoresecretz/ghoeq/server/stream"
)

var SpawnIDIndex = &memdb.UintFieldIndex{
	Field: "SpawnID",
}

var StreamIDIndex = &memdb.StringFieldIndex{
	Field: "StreamID",
}

var SpawnID_StreamID_IndexSchema = &memdb.IndexSchema{
	Name:   "SpawnID-StreamID",
	Unique: true,
	Indexer: &memdb.CompoundIndex{
		Indexes: []memdb.Indexer{
			SpawnIDIndex,
			StreamIDIndex,
		},
	},
}

var SpawnID_IndexSchema = &memdb.IndexSchema{
	Name:    "StreamID",
	Unique:  false,
	Indexer: StreamIDIndex,
}

var dbSchema = &memdb.DBSchema{
	Tables: map[string]*memdb.TableSchema{
		"spawns": {
			Name: "spawns",
			Indexes: map[string]*memdb.IndexSchema{
				"SpawnID-StreamID": SpawnID_StreamID_IndexSchema,
				"StreamID":         SpawnID_IndexSchema,
			},
		},
		"spawnUpdates": {
			Name: "spawnUpdates",
			Indexes: map[string]*memdb.IndexSchema{
				"SpawnID-StreamID-Type": {
					Name:   "SpawnID-StreamID-Type",
					Unique: true,
					Indexer: &memdb.CompoundIndex{
						Indexes: []memdb.Indexer{
							SpawnIDIndex,
							StreamIDIndex,
							&memdb.UintFieldIndex{
								Field: "UpdateType",
							},
						},
					},
				},
				"SpawnID-StreamID": SpawnID_StreamID_IndexSchema,
				"StreamID":         SpawnID_IndexSchema,
			},
		},
	},
}

type DB struct {
	db *memdb.MemDB
}

type Spawn struct {
	*eqStruct.ZoneSpawn
	SpawnID    uint16
	StreamID   string
	LastUpdate time.Time
}

type SpawnUpdate struct {
	eqStruct.EQStruct
	SpawnID    uint16
	StreamID   string
	LastUpdate time.Time
	UpdateType UpdateType
}

type UpdateType uint

const (
	SUT_Unknown UpdateType = iota
	SUT_Position
)

func newDB() (*DB, error) {
	db, err := memdb.NewMemDB(dbSchema)
	if err != nil {
		return nil, err
	}

	return &DB{db: db}, nil
}

func (d *DB) Update(p *stream.StreamPacket) error {
	if p.Obj == nil {
		return nil
	}

	switch t := p.Obj.(type) {
	case *eqStruct.ZoneSpawns:
		return d.UpdateSpawns(p, t)
	case *eqStruct.ZoneSpawn:
		return d.UpdateSpawn(p, t, nil)
	case *eqStruct.DeleteSpawn:
		return d.DeleteSpawn(p, t)
	case *eqStruct.SpawnPositionUpdates:
		return d.UpdateSpawnPositions(p, t)
	case *eqStruct.SpawnPositionUpdate:
		return d.SpawnUpdate(p, t, SUT_Position, nil)
	}

	return nil
}

func (d *DB) UpdateSpawns(p *stream.StreamPacket, z *eqStruct.ZoneSpawns) error {
	txn := d.db.Txn(true)

	for _, s := range z.Spawns {
		if err := d.UpdateSpawn(p, s, txn); err != nil {
			return err
		}
	}

	return nil
}

func (d *DB) DeleteSpawn(p *stream.StreamPacket, s *eqStruct.DeleteSpawn) error {
	txn := d.db.Txn(true)

	sp := Spawn{
		SpawnID:  s.SpawnID,
		StreamID: p.Stream.ID,
	}
	if err := txn.Delete("spawns", sp); err != nil {
		return err
	}
	if _, err := txn.DeleteAll("spawnUpdates", "SpawnID-StreamID", sp); err != nil {
		return err
	}

	txn.Commit()

	return nil
}

func (d *DB) UpdateSpawn(p *stream.StreamPacket, z *eqStruct.ZoneSpawn, itxn *memdb.Txn) error {
	var txn *memdb.Txn
	if itxn == nil {
		txn = itxn
	} else {
		txn = d.db.Txn(true)
	}

	if err := txn.Insert("spawns", Spawn{
		ZoneSpawn:  z,
		StreamID:   p.Stream.ID,
		LastUpdate: p.Origin,
	}); err != nil {
		return fmt.Errorf("failed inserting spawn (%v): %w", z, err)
	}

	if itxn == nil {
		txn.Commit()
	}

	return nil
}

func (d *DB) UpdateSpawnPositions(p *stream.StreamPacket, z *eqStruct.SpawnPositionUpdates) error {
	txn := d.db.Txn(true)

	for _, s := range z.Updates {
		if err := d.SpawnUpdate(p, s, SUT_Position, txn); err != nil {
			return err
		}
	}

	return nil
}

func (d *DB) SpawnUpdate(p *stream.StreamPacket, z eqStruct.EQStruct, uType UpdateType, itxn *memdb.Txn) error {
	var txn *memdb.Txn
	if itxn == nil {
		txn = itxn
	} else {
		txn = d.db.Txn(true)
	}

	if err := txn.Insert("spawns", SpawnUpdate{
		EQStruct:   z,
		StreamID:   p.Stream.ID,
		LastUpdate: p.Origin,
		UpdateType: uType,
	}); err != nil {
		return fmt.Errorf("failed inserting spawn (%v): %w", z, err)
	}

	if itxn == nil {
		txn.Commit()
	}

	return nil
}
