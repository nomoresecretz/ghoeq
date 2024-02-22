package eqStruct

type LoginInfo struct {
	LoginInfo []byte // 000
	Account   string
	Password  string
	Zoning    uint8 // 192

	bPointer int
}

func (p *LoginInfo) EQType() EQType { return EQT_LoginInfo }
func (p *LoginInfo) bp() *int       { return &p.bPointer }

func (p *LoginInfo) Deserialize(b []byte) error {
	p.bPointer = 0

	if err := EQReadStringNullTerm(b, p, &p.Account); err != nil {
		return err
	}
	
	if err := EQReadStringNullTerm(b, p, &p.Password); err != nil {
		return err
	}

	p.bPointer = 192
	if err := EQRead(b, p, &p.Zoning, 4); err != nil {
		return err
	}

	return nil
}
