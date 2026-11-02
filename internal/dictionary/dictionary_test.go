package dictionary

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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

func TestTextNormalization(t *testing.T) {
	for input, want := range map[string]Entry{
		"女儿\tnü er\t3\n":    {Text: "女儿", Code: "nv er", Count: 3},
		"女儿\tnu:'er\t3\n":   {Text: "女儿", Code: "nv er", Count: 3},
		"  女儿 \t nv  er \n": {Text: "女儿", Code: "nv er", Count: 1},
		"\r\n女儿\tnv er\r\n": {Text: "女儿", Code: "nv er", Count: 1},
		"女儿\tnv er\t0\n":    {Text: "女儿", Code: "nv er", Count: 0},
	} {
		b, err := Read(strings.NewReader(input))
		if err != nil || len(b.Words) != 1 || b.Words[0] != want {
			t.Fatalf("%q: %v %+v", input, err, b.Words)
		}
	}
	// A deletion tombstone alone is an empty export, not an error.
	if _, err := Read(strings.NewReader("ni\t泥\tc=-1 d=0 t=2\n")); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
}

func TestTextLimits(t *testing.T) {
	long := strings.Repeat("字", 3000) + "\tzi\t1\n" // one line over the scanner limit
	for name, input := range map[string]string{
		"long line":     long,
		"long syllable": "字\tabcdefghi\t1\n",
		"long text":     strings.Repeat("字", 1400) + "\tzi\t1\n",
		"four fields":   "字\tzi\t1\textra\n",
		"control":       "字\u0085字\tzi zi\t1\n",
		"invalid UTF-8": "\xff\tzi\t1\n",
	} {
		if _, err := Read(strings.NewReader(input)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	var b strings.Builder
	for range MaxEntries + 1 {
		b.WriteString("字\tzi\t1\n")
	}
	if _, err := Read(strings.NewReader(b.String())); err == nil {
		t.Fatal("accepted more than MaxEntries")
	}
	if _, err := Read(io.LimitReader(zeros{}, MaxBytes+1)); err == nil {
		t.Fatal("accepted a file over MaxBytes")
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func TestSCELRejectsMalformedTables(t *testing.T) {
	good := scelFixture(0x44)
	mutate := func(f func(b []byte) []byte) []byte {
		return f(append([]byte(nil), good...))
	}
	for name, b := range map[string][]byte{
		"version": mutate(func(b []byte) []byte { b[4] = 0x46; return b }),
		"duplicate syllable": mutate(func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[0x1544+4+4:], 0) // "hao" reuses id 0
			return b
		}),
		"unknown syllable": mutate(func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[0x2628+4+2:], 9)
			return b
		}),
		"odd code length": mutate(func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[0x2628+2:], 3)
			return b
		}),
		"lone surrogate": mutate(func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[0x2628+8+2:], 0xdc00)
			return b
		}),
		"short extension": mutate(func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[0x2628+8+2+4:], 1)
			return b
		}),
		"syllable with symbol": mutate(func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[0x1544+4:], '!')
			return b
		}),
	} {
		if _, err := Read(bytes.NewReader(b)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	// A surrogate pair is a valid supplementary-plane character.
	pair := mutate(func(b []byte) []byte {
		binary.LittleEndian.PutUint16(b[0x2628+8+2:], 0xd840)
		binary.LittleEndian.PutUint16(b[0x2628+8+4:], 0xdc00)
		return b
	})
	if got, err := Read(bytes.NewReader(pair)); err != nil || got.Words[0].Text != "\U00020000" {
		t.Fatalf("surrogate pair: %v %+v", err, got.Words)
	}
}

func TestBackupRejectsInvalidSettingsData(t *testing.T) {
	for _, input := range []string{
		`{"format":"shixin-dictionary","version":1,"pins":[{"scheme":"nope","code":"ni","text":"你"}]}`,
		`{"format":"shixin-dictionary","version":1,"pins":[{"scheme":"rime_ice","code":"NI","text":"你"}]}`,
		`{"format":"shixin-dictionary","version":1,"words":[{"text":"你","code":"ni","count":-1}]}`,
		`{"format":"shixin-dictionary","version":1,"words":[{"text":"你","code":" ni","count":1}]}`,
		`{"format":"shixin-dictionary","version":1}` + "\xff",
		`{"format":"other","version":1}`,
		// A leading # would turn the exported table line into a comment.
		`{"format":"shixin-dictionary","version":1,"words":[{"text":"#你","code":"ni","count":1}]}`,
	} {
		if _, err := Read(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestWritersRejectInvalidData(t *testing.T) {
	var out bytes.Buffer
	if err := WriteTable(&out, []Entry{{Text: "a\tb", Code: "ni", Count: 1}}); err == nil {
		t.Fatal("table accepted a tab in text")
	}
	if err := WriteTable(&out, []Entry{{Text: "你", Code: "ni\nbad", Count: 1}}); err == nil {
		t.Fatal("table accepted a newline in code")
	}
	words := make([]Entry, 0, 1<<20)
	for range cap(words) {
		words = append(words, Entry{Text: strings.Repeat("字", 30), Code: "zi", Count: 1})
	}
	if err := WriteBackup(io.Discard, Backup{Words: words}); err == nil {
		t.Fatal("backup exceeded MaxBytes")
	}
	// The table writer reports write failures instead of a short file.
	if err := WriteTable(failWriter{}, []Entry{{Text: "你", Code: "ni", Count: 1}}); err == nil {
		t.Fatal("write failure hidden")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
