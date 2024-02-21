package decoder

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type OpCode uint16

type decoder struct {
	mu sync.RWMutex
	om map[OpCode]string
	mo map[string]OpCode
}

func (d *decoder) GetOp(op OpCode) string {
	d.mu.RLock()
	s, ok := d.om[op]
	d.mu.RUnlock()
	if !ok {
		return ""
	}
	return s
}

func (d *decoder) GetOpByName(op string) OpCode {
	d.mu.RLock()
	s, ok := d.mo[op]
	d.mu.RUnlock()
	if !ok {
		return 0
	}
	return s
}

// NewDecoder returns an opcode to name decoder, possibly other functions in the future like deserialization.
func NewDecoder() *decoder {
	return &decoder{
		om: make(map[OpCode]string),
		mo: make(map[string]OpCode),
	}
}

// LoadMap takes a map file containinig Name=0x0000 representations of current opcode mappings for processing.
func (d *decoder) LoadMap(file string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	r, err := os.OpenFile(file, os.O_RDONLY, 0o755)
	if err != nil {
		return err
	}
	defer r.Close()
	re := regexp.MustCompile(`^([a-zA-Z_0-9]+)=([a-fA-F0-9x]+)`)
	s := bufio.NewScanner(r)
	for s.Scan() {
		sl := re.FindStringSubmatch(s.Text())
		if len(sl) == 0 {
			continue
		}
		opCode, err := hextouint(sl[2])
		if err != nil {
			return err
		}
		d.om[opCode] = sl[1]
		d.mo[sl[1]] = opCode
	}
	return s.Err()
}

func hextouint(s string) (OpCode, error) {
	clean := strings.Replace(s, "0x", "", -1)
	opCode, err := strconv.ParseUint(clean, 16, 16)
	return OpCode(opCode), err
}

func (o *OpCode) String() string {
	return fmt.Sprintf("%#4x", uint16(*o))
}
