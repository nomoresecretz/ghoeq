package eqStruct

type EnterWorld struct {
	Name string // 000

	bPointer int
}

func (p *EnterWorld) EQType() EQType { return EQT_EnterWorld }
func (p *EnterWorld) bp() *int       { return &p.bPointer }

func (p *EnterWorld) Unmarshal(b []byte) error {
	p.bPointer = 0

	if err := EQRead(b, p, &p.Name, 64); err != nil {
		return err
	}
	return nil
}
