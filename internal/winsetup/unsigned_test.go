package winsetup

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"testing"
)

func TestRequireUnsigned(t *testing.T) {
	data := testBundle(t)
	if err := RequireUnsigned(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	at := 128 + 4 + binary.Size(pe.FileHeader{}) + 112 + 4*8
	for _, c := range []struct{ offset, size uint32 }{{uint32(len(data)), 128}, {uint32(len(data)), 0}, {0, 128}} {
		bad := bytes.Clone(data)
		binary.LittleEndian.PutUint32(bad[at:], c.offset)
		binary.LittleEndian.PutUint32(bad[at+4:], c.size)
		if err := RequireUnsigned(bytes.NewReader(bad)); err == nil {
			t.Fatal("accepted a signed or malformed certificate boundary")
		}
	}
	if err := RequireUnsigned(bytes.NewReader([]byte("not a PE file"))); err == nil {
		t.Fatal("accepted invalid executable")
	}
}
