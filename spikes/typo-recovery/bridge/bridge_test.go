package bridge

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"testing"
)

func fixture() Snapshot {
	var s Snapshot
	binary.LittleEndian.PutUint32(s[:], Version)
	binary.LittleEndian.PutUint32(s[4:], SnapshotSize)
	binary.LittleEndian.PutUint32(s[8:], 1)
	r := s[HeaderSize : HeaderSize+RecordSize]
	for i, v := range []uint64{0x0102030405060708, 11, 13, 17} {
		binary.LittleEndian.PutUint64(r[i*8:], v)
	}
	binary.LittleEndian.PutUint32(r[44:], 5)
	binary.LittleEndian.PutUint32(r[48:], 1)
	binary.LittleEndian.PutUint32(r[52:], 3)
	binary.LittleEndian.PutUint32(r[56:], 4)
	binary.LittleEndian.PutUint32(r[60:], math.Float32bits(10))
	binary.LittleEndian.PutUint32(r[64:], 6)
	copy(r[72:], "你好")
	return s
}
func TestCodec(t *testing.T) {
	s := fixture()
	if !ValidSnapshot(s[:]) {
		t.Fatal("valid fixture rejected")
	}
	r, ok := s.Record(0)
	if !ok || string(r.Text) != "你好" || r.End != 5 || r.EditBegin != 3 || r.Cost != 10 || binary.LittleEndian.Uint64(r.Token[:]) != 0x0102030405060708 {
		t.Fatalf("bad decode: %+v", r)
	}
	if _, ok := s.Record(1); ok {
		t.Fatal("out of range")
	}
	mutations := []func(*Snapshot){
		func(s *Snapshot) { s[0] = 2 }, func(s *Snapshot) { s[4] = 0 }, func(s *Snapshot) { s[8] = 9 }, func(s *Snapshot) { s[12] = 1 },
		func(s *Snapshot) { s[HeaderSize+32] = 1 }, func(s *Snapshot) { s[HeaderSize+39] = 1 },
		func(s *Snapshot) { s[HeaderSize+68] = 1 }, func(s *Snapshot) { s[HeaderSize+44] = 33 },
		func(s *Snapshot) { s[HeaderSize+48] = 4 }, func(s *Snapshot) { s[HeaderSize+56] = 5 },
		func(s *Snapshot) { s[HeaderSize+64] = 0 }, func(s *Snapshot) { s[HeaderSize+65] = 2 },
		func(s *Snapshot) { s[HeaderSize+72] = 0xff }, func(s *Snapshot) { s[SnapshotSize-1] = 1 },
		func(s *Snapshot) {
			binary.LittleEndian.PutUint32(s[HeaderSize+60:], math.Float32bits(float32(math.Inf(1))))
		},
		func(s *Snapshot) {
			binary.LittleEndian.PutUint32(s[HeaderSize+60:], math.Float32bits(float32(math.NaN())))
		},
	}
	for i, mutate := range mutations {
		s := fixture()
		mutate(&s)
		if ValidSnapshot(s[:]) {
			t.Errorf("mutation %d accepted", i)
		}
	}
	if ValidSnapshot(s[:SnapshotSize-1]) || ValidSnapshot(nil) {
		t.Fatal("short frame accepted")
	}
	for _, offset := range []int{0, 8, 16, 24} {
		s := fixture()
		clear(s[HeaderSize+offset : HeaderSize+offset+8])
		if ValidSnapshot(s[:]) {
			t.Fatalf("zero identity %d", offset)
		}
	}
	s = fixture()
	s[8] = 2
	copy(s[HeaderSize+RecordSize:], s[HeaderSize:HeaderSize+RecordSize])
	s[HeaderSize+RecordSize+32] = 1
	if !ValidSnapshot(s[:]) {
		t.Fatal("two records rejected")
	}
	s[HeaderSize+RecordSize+16]++
	if ValidSnapshot(s[:]) {
		t.Fatal("mixed revisions accepted")
	}
}
func TestFallbackAndAllocations(t *testing.T) {
	if _, err := Attach(func(string) (uintptr, error) { return 0, errors.New("absent") }); err != ErrUnsupported {
		t.Fatal(err)
	}
	var native Native
	s := fixture()
	var token Token
	if native.Read(1, &s) || s != (Snapshot{}) || native.Configure(1, 1) || native.Select(1, &token) || native.Invalidate(1) {
		t.Fatal("zero-value bridge did not fail closed")
	}
	s = fixture()
	var failed bool
	allocs := testing.AllocsPerRun(1000, func() {
		if !ValidSnapshot(s[:]) {
			failed = true
		}
		if _, ok := s.Record(0); !ok {
			failed = true
		}
	})
	if failed || allocs != 0 {
		t.Fatalf("failed=%v allocations=%v", failed, allocs)
	}
}
func TestNativeExportFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/native-snapshot.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidSnapshot(data) {
		t.Fatal("native frame rejected")
	}
	var s Snapshot
	copy(s[:], data)
	found := false
	for i := 0; i < s.Count(); i++ {
		r, ok := s.Record(i)
		if !ok {
			t.Fatal(i)
		}
		if string(r.Text) == "你好" {
			found = true
			if r.EditBegin != 3 || r.EditEnd != 4 || r.Cost != 10 {
				t.Fatalf("bad native edit: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("native fixture lacks expected correction")
	}
}
func FuzzSnapshot(f *testing.F) {
	s := fixture()
	f.Add(s[:])
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if ValidSnapshot(data) {
			var s Snapshot
			copy(s[:], data)
			for i := 0; i < s.Count(); i++ {
				if _, ok := s.Record(i); !ok {
					t.Fatal("inconsistent decode")
				}
			}
		}
	})
}

func BenchmarkSnapshotDecode(b *testing.B) {
	s := fixture()
	b.ReportAllocs()
	for b.Loop() {
		if !ValidSnapshot(s[:]) {
			b.Fatal("invalid")
		}
		if _, ok := s.Record(0); !ok {
			b.Fatal("decode")
		}
	}
}
