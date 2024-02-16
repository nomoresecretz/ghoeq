package cypher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func fileLoader(file string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		return nil, err
	}

	return data, err
}

func testData(t *testing.T, file string) []byte {
	t.Helper()
	data, err := fileLoader(file)
	if err != nil {
		t.Fatalf("broken test: %s", err)
	}
	return []byte(data)
}

func TestWalk(t *testing.T) {
	tests := []struct {
		name     string
		bufFile  string
		wantFile string
		want     []byte
		cypher   Cypher
	}{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bs := testData(t, tc.bufFile)
			cb := NewCBox(len(bs)-1, tc.cypher)
			cb.Fill(bs[:])
			cb.Flip()
			cb.Seed()
			for k := 0; k < cb.Len(); k++ {
				cb.Walk()
			}
			var want []byte
			if tc.wantFile != "" {
				want = testData(t, tc.wantFile)
			} else {
				want = tc.want
			}
			got := cb.Dump(0)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("func diff(-want,+got):%v", diff)
			}
		})
	}
}

func TestWalkBack(t *testing.T) {
	tests := []struct {
		name     string
		have     []byte
		bufFile  string
		wantFile string
		want     []byte
		cypher   Cypher
	}{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bs := tc.have
			if tc.bufFile != "" {
				bs = testData(t, tc.bufFile)
			}
			cb := NewCBox(len(bs), tc.cypher)
			cb.Fill(bs)
			for k := 0; k < cb.Len(); k++ {
				cb.WalkInvert()
			}
			var want []byte
			if tc.wantFile != "" {
				want = testData(t, tc.wantFile)
			} else {
				want = tc.want
			}
			cb.Flip()
			got := cb.Dump(0)
			if diff := cmp.Diff(got[:len(want)], want); diff != "" {
				t.Errorf("func diff(-got,+want):%v", diff)
			}
		})
	}
}
