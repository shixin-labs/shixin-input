package dictionary

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"shixin/internal/settings"
)

func TestTextMigration(t *testing.T) {
	for _, input := range []string{
		"# Rime export\n你好\tni hao\t12\n",
		"ni'hao\t你好\t12\n",
		"你好\tNI HAO\t12\n",
		"# Rime snapshot\nni hao \t你好\tc=12 d=0.5 t=20\nni \t泥\tc=-1 d=0 t=2\n",
		"\ufeff---\nname: external\nversion: '1'\n...\n你好\tni hao\t12\n",
	} {
		b, err := Read(strings.NewReader(input))
		want := []Entry{{Text: "你好", Code: "ni hao", Count: 12}}
		if err != nil || !reflect.DeepEqual(b.Words, want) {
			t.Fatalf("migration: %v %+v", err, b.Words)
		}
	}
}

func TestBackupRoundTrip(t *testing.T) {
	want := Backup{Format: "shixin-dictionary", Version: 1, Words: []Entry{{Text: "女儿", Code: "nv er", Count: 5}},
		Phrases: []settings.Phrase{{Code: "yx", Text: "邮箱"}}, Pins: []settings.Pin{{Scheme: "double_pinyin", Code: "ui", Text: "是"}}}
	var out bytes.Buffer
	if err := WriteBackup(&out, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&out)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %v %+v", err, got)
	}
}

func TestEmptyBackupCanBeRestored(t *testing.T) {
	var out bytes.Buffer
	if err := WriteBackup(&out, Backup{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(&out); err != nil {
		t.Fatal("an empty portable backup must remain restorable", err)
	}
}

func scelFixture(version byte) []byte {
	offset := 0x2628
	if version == 0x45 {
		offset = 0x26c4
	}
	b := make([]byte, offset)
	copy(b, []byte{0x40, 0x15, 0, 0, version})
	putString := func(dst []byte, s string) []byte {
		for _, v := range utf16.Encode([]rune(s)) {
			dst = binary.LittleEndian.AppendUint16(dst, v)
		}
		return dst
	}
	p := 0x1544
	for i, s := range []string{"ni", "hao"} {
		binary.LittleEndian.PutUint16(b[p:], uint16(i))
		binary.LittleEndian.PutUint16(b[p+2:], uint16(len(s)*2))
		copy(b[p+4:], putString(nil, s))
		p += 4 + len(s)*2
	}
	for _, n := range []uint16{1, 4, 0, 1, 4} {
		b = binary.LittleEndian.AppendUint16(b, n)
	}
	b = putString(b, "你好")
	b = binary.LittleEndian.AppendUint16(b, 10)
	b = binary.LittleEndian.AppendUint16(b, 42)
	return append(b, make([]byte, 8)...)
}

func TestSCELMigrationAndTruncation(t *testing.T) {
	for _, v := range []byte{0x44, 0x45} {
		b := scelFixture(v)
		got, err := Read(bytes.NewReader(b))
		if err != nil || len(got.Words) != 1 || got.Words[0] != (Entry{Text: "你好", Code: "ni hao", Count: 42}) {
			t.Fatalf("SCEL %x: %v %+v", v, err, got)
		}
		for _, end := range []int{4, 0x1544, len(b) - 1, len(b) - 9} {
			if _, err := Read(bytes.NewReader(b[:end])); err == nil {
				t.Fatalf("accepted truncated SCEL at %d", end)
			}
		}
	}
}

func TestRejectMalformedDictionary(t *testing.T) {
	for _, input := range []string{"", "word only", "私密内容\tbad!code\t3", "私密内容\tni\t-1", "私密内容\tni\tbogus", "私密内容\tni\t1000000001", "{}", `{"format":"shixin-dictionary","version":2}`, "---\nname: unfinished", "私密\x00内容\tni\t1"} {
		_, err := Read(strings.NewReader(input))
		if err == nil {
			t.Fatalf("accepted malformed input")
		}
		if strings.Contains(err.Error(), "私密") {
			t.Fatal("error exposed imported text")
		}
	}
	if _, err := Read(strings.NewReader("# empty export")); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
}

func FuzzDictionary(f *testing.F) {
	f.Add([]byte("你好\tni hao\t12\n"))
	f.Add(scelFixture(0x44))
	f.Add([]byte(`{"format":"shixin-dictionary","version":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			t.Skip()
		}
		got, err := Read(bytes.NewReader(b))
		if err != nil {
			return
		}
		var out bytes.Buffer
		if err := WriteBackup(&out, got); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(&out); err != nil {
			t.Fatal("accepted dictionary cannot round-trip", err)
		}
	})
}
