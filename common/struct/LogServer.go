package eqstruct

type PlayEverquestResponse struct {
	bPointer int
}

func (p *PlayEverquestResponse) EQType() EQType { return EQT_PlayEverquestResponse }
func (p *PlayEverquestResponse) bp() *int       { return &p.bPointer }

func (p *PlayEverquestResponse) Deserialize(b []byte) (*PlayEverquestResponse, error) {
	p.bPointer = 0

	return p, nil
}
