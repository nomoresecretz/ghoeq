package eqStruct

type LogServer struct {
	ShortName string // 032 Max32
	Address   string // 112 Max16
	Port      uint32 // 176

	bPointer int
}

func (p *LogServer) EQType() EQType { return EQT_LogServer }
func (p *LogServer) bp() *int       { return &p.bPointer }

func (p *LogServer) Unmarshal(b []byte) error {
	p.bPointer = 32
	if err := EQRead(b, p, &p.ShortName, 32); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Address, 32); err != nil {
		return err
	}

	if err := EQRead(b, p, &p.Port, 32); err != nil {
		return err
	}

	return nil
}
