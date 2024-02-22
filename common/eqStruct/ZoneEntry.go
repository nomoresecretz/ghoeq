package eqStruct

type ServerZoneEntry struct {
	Checksum  []byte  // 000 Len4
	Type      uint8   // 004
	Name      string  // 005 MAX64
	Unknown70 []byte  // 70
	ZoneId    uint32  // 72
	PosY      float32 // 0076
	PosX      float32 // 0080
	PosZ      float32 // 0084
	Heading   float32 // 0088
	MyChar    int32   // 140
	HPMax     uint32  // 156
	HPCur     uint32  // 160
	GuildId   uint16  // 164
	Class     uint8   // 173
	Race      uint16  // 174
	Gender    uint8   // 176
	Level     uint8   // 177
	Invis     uint8   // 178
	Sneaking  uint8   // 179
	PVP       uint8   // 180

	bPointer int
}

func (p *ServerZoneEntry) EQType() EQType { return EQT_ServerZoneEntry }
func (p *ServerZoneEntry) bp() *int       { return &p.bPointer }

func (p *ServerZoneEntry) Unmarshal(b []byte) error {
	p.bPointer = 0

	if err := EQRead(b, p, &p.Checksum, 4); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Type, 0); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Name, 64); err != nil {
		return err
	}

	p.bPointer = 70
	if err := EQRead(b, p, &p.Unknown70, 2); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.ZoneId, 2); err != nil {
		return err
	}

	/*
		if err := EQRead(b, p, &p.PosX, 2); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.PosY, 2); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.PosZ, 2); err != nil {
			return err
		}
		if err := EQRead(b, p, &p.Heading, 2); err != nil {
			return err
		}
	*/

	p.bPointer = 156
	if err := EQRead(b, p, &p.HPMax, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.HPCur, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.GuildId, 2); err != nil {
		return err
	}

	p.bPointer = 173
	if err := EQRead(b, p, &p.Class, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.Race, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.Gender, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.Level, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.Invis, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.Sneaking, 2); err != nil {
		return err
	}
	if err := EQRead(b, p, &p.PVP, 2); err != nil {
		return err
	}

	return nil
}
