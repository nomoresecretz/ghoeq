package eqStruct

type PlayRequest struct {
	IP      string // 000
	bPointer int
}

func (p *PlayRequest) EQType() EQType { return EQT_PlayRequest }
func (p *PlayRequest) bp() *int       { return &p.bPointer }

func (p *PlayRequest) Deserialize(b []byte) error {
	p.bPointer = 0

	if err := EQReadStringNullTerm(b, p, &p.IP); err != nil {
		return err
	}

	return nil
}
