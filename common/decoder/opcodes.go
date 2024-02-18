package decoder

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type decoder struct {
	mu sync.RWMutex
	om map[uint16]string
}

func (d *decoder) GetOp(op uint16) string {
	d.mu.RLock()
	s := d.om[op]
	d.mu.RUnlock()
	return s
}

// NewDecoder returns an opcode to name decoder, possibly other functions in the future like deserialization.
func NewDecoder() *decoder {
	return &decoder{
		om: make(map[uint16]string),
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
	re := regexp.MustCompile(`^([a-zA-Z_]+)=([a-fA-F0-9x]+)`)
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
	}
	return s.Err()
}

func hextouint(s string) (uint16, error) {
	clean := strings.Replace(s, "0x", "", -1)
	opCode, err := strconv.ParseUint(clean, 16, 16)
	return uint16(opCode), err
}
