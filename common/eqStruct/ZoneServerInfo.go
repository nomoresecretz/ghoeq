package eqStruct

type ZoneServerInfo struct {
	IP      string // 000
	Port     uint16 // 128
	bPointer int
}

func (p *ZoneServerInfo) EQType() EQType { return EQT_ZoneServerInfo }
func (p *ZoneServerInfo) bp() *int       { return &p.bPointer }

func (p *ZoneServerInfo) Deserialize(b []byte) error {
	p.bPointer = 0

	if err := EQReadString(b, p, &p.IP, 128); err != nil {
		return err
	}

	if err := EQReadUint16(b, p, &p.Port); err != nil {
		return err
	}

	return nil
}
