package eqStruct

type PlayRequest struct {
	IP       string // 000
	bPointer int
}

func (p *PlayRequest) EQType() EQType { return EQT_PlayRequest }
func (p *PlayRequest) bp() *int       { return &p.bPointer }

func (p *PlayRequest) Unmarshal(b []byte) error {
	p.bPointer = 0

	if err := EQRead(b, p, &p.IP, 0); err != nil {
		return err
	}

	return nil
}
