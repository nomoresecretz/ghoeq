package eqStruct

type LoginAccepted struct {
	Account string // 00 Max 10

	bPointer int
}

func (p *LoginAccepted) EQType() EQType { return EQT_LoginAccepted }
func (p *LoginAccepted) bp() *int       { return &p.bPointer }

func (p *LoginAccepted) Deserialize(b []byte) error {
	p.bPointer = 0
	if err := EQReadString(b, p, &p.Account, 10); err != nil {
		return err
	}

	return nil
}
