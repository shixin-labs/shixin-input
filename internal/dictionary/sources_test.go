package dictionary

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

type sogouRecord struct {
	freq  uint16
	codes []uint16
	text  string
}

// sogouFixture lays records out as Sogou for Mac's sgim_gd_usr.bin does,
// with the index at 0x8c and a byte capacity of four bytes per record.
func sogouFixture(records ...sogouRecord) []byte {
	const index, capacity = 0x8c, 64
	var data []byte
	var offsets []uint32
	for _, r := range records {
		offsets = append(offsets, uint32(len(data)))
		data = binary.LittleEndian.AppendUint16(data, r.freq)
		data = append(data, 3, 0, 0, 0, 1, 0, 2)
		data = binary.LittleEndian.AppendUint16(data, uint16(2*len(r.codes)))
		for _, c := range r.codes {
			data = binary.LittleEndian.AppendUint16(data, c)
		}
		text := utf16.Encode([]rune(r.text))
		data = binary.LittleEndian.AppendUint16(data, uint16(2+2*len(text)+2+2*len(r.codes)))
		data = binary.LittleEndian.AppendUint16(data, uint16(2*len(text)))
		for _, v := range text {
			data = binary.LittleEndian.AppendUint16(data, v)
		}
		// The codes as typed.
		data = binary.LittleEndian.AppendUint16(data, uint16(2*len(r.codes)))
		for _, c := range r.codes {
			data = binary.LittleEndian.AppendUint16(data, c)
		}
	}
	dataStart, dataCap := index+capacity, len(data)+32
	b := make([]byte, dataStart+dataCap)
	copy(b, "SGPU")
	put := func(p, v int) { binary.LittleEndian.PutUint32(b[p:], uint32(v)) }
	put(0x10, len(b))
	put(0x38, index)
	put(0x3c, capacity)
	put(0x40, len(records))
	put(0x44, dataStart)
	put(0x48, dataCap)
	put(0x4c, len(data))
	for i, o := range offsets {
		put(index+4*i, int(o))
	}
	copy(b[dataStart:], data)
	return b
}

func TestSogouUserDictionary(t *testing.T) {
	// 219 ni, 117 hao, 300 shi, 353 xin (Sogou's numbering); 842 is an abbreviation.
	b := sogouFixture(
		sogouRecord{2, []uint16{219, 117}, "你好"},
		sogouRecord{0, []uint16{219}, "你"},       // forgotten
		sogouRecord{5, []uint16{842, 117}, "私密"}, // abbreviated code
		sogouRecord{7, []uint16{300, 353}, "拾新"},
	)
	got, err := Read(bytes.NewReader(b))
	want := []Entry{{Text: "你好", Code: "ni hao", Count: 2}, {Text: "拾新", Code: "shi xin", Count: 7}}
	if err != nil || !reflect.DeepEqual(got.Words, want) {
		t.Fatalf("got %v %+v", err, got.Words)
	}
	if sogouSyllables[219] != "ni" || sogouSyllables[117] != "hao" || sogouSyllables[300] != "shi" || sogouSyllables[353] != "xin" || len(sogouSyllables) != 413 {
		t.Fatal("Sogou's syllable numbering changed")
	}

	// Sogou rewriting the file (length mismatch), truncation and a record
	// pointing past the used data all fail as a whole.
	for _, broken := range [][]byte{b[:len(b)-1], b[:0x60], append(clone(b), 0)} {
		if _, err := Read(bytes.NewReader(broken)); err == nil {
			t.Fatal("accepted a torn Sogou dictionary")
		}
	}
	bad := clone(b)
	binary.LittleEndian.PutUint32(bad[0x8c:], 1<<20)
	if _, err := Read(bytes.NewReader(bad)); err == nil || strings.Contains(err.Error(), "私密") {
		t.Fatalf("bad offset: %v", err)
	}
	if _, err := Read(bytes.NewReader(sogouFixture(sogouRecord{0, []uint16{219}, "你"}))); !errors.Is(err, ErrEmpty) {
		t.Fatalf("only forgotten words: %v", err)
	}
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }

// Real .scel tables continue with letters after the syllables, and words
// spelled with them are skipped instead of failing the file.
func TestSCELLetterCodes(t *testing.T) {
	b := scelFixture(0x45)
	p := 0x1544 + 4 + 4 + 4 + 6 // after "ni" and "hao"
	binary.LittleEndian.PutUint16(b[p:], 2)
	binary.LittleEndian.PutUint16(b[p+2:], 2)
	binary.LittleEndian.PutUint16(b[p+4:], 'A')
	// A second group, "A股" spelled A ni.
	for _, n := range []uint16{1, 4, 2, 0} {
		b = binary.LittleEndian.AppendUint16(b, n)
	}
	b = binary.LittleEndian.AppendUint16(b, 4)
	for _, v := range utf16.Encode([]rune("A股")) {
		b = binary.LittleEndian.AppendUint16(b, v)
	}
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b[0x120] = 2
	// Sogou appends other tables after the counted groups.
	b = append(b, "DELTBL"...)
	got, err := Read(bytes.NewReader(b))
	if err != nil || len(got.Words) != 1 || got.Words[0].Text != "你好" {
		t.Fatalf("got %v %+v", err, got.Words)
	}
	if name := SCELName(b); name != "" {
		t.Fatalf("untitled fixture named %q", name)
	}
	title := utf16.Encode([]rune("网络流行新词"))
	for i, v := range title {
		binary.LittleEndian.PutUint16(b[0x130+2*i:], v)
	}
	if name := SCELName(b); name != "网络流行新词" {
		t.Fatalf("title %q", name)
	}
}

func TestReadRimeTable(t *testing.T) {
	in := "# Rime user dictionary\n#@/db_name\tluna_pinyin\n你好\tni hao\t3\n五笔\twq!\t1\n词\tci\tc=-1 d=0 t=1\n拾新\tshi xin\tc=4 d=0.5 t=9\n"
	got, err := ReadRimeTable(strings.NewReader(in))
	want := []Entry{{Text: "你好", Code: "ni hao", Count: 3}, {Text: "拾新", Code: "shi xin", Count: 4}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %+v", err, got)
	}
	if _, err := ReadRimeTable(strings.NewReader("# only comments\n")); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
}

func TestFindSources(t *testing.T) {
	home, appData := t.TempDir(), t.TempDir()
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sogou := filepath.Join(home, "Library", "Application Support", "Sogou", "InputMethod", "SogouPY")
	write(filepath.Join(sogou, "sgim_gd_usr.bin"), sogouFixture(sogouRecord{2, []uint16{219, 117}, "你好"}))
	write(filepath.Join(sogou, "scd", "4.scel"), scelFixture(0x45))
	write(filepath.Join(sogou, "scd", "broken.scel"), []byte("not a cell dictionary"))
	squirrel := filepath.Join(home, "Library", "Rime")
	write(filepath.Join(squirrel, "luna_pinyin.userdb", "CURRENT"), []byte("MANIFEST-000001\n"))
	write(filepath.Join(squirrel, "wubi86.userdb", "CURRENT"), []byte("MANIFEST-000001\n"))
	write(filepath.Join(squirrel, "rime_ice.userdb", "LOCK"), nil) // no CURRENT: not a database
	write(filepath.Join(appData, "Rime", "rime_ice.userdb", "CURRENT"), []byte("MANIFEST-000001\n"))

	got := FindSources(Env{GOOS: "darwin", Home: home})
	want := []Source{
		{ID: "sogou:user", IME: "sogou", Path: filepath.Join(sogou, "sgim_gd_usr.bin"), Words: 1},
		{ID: "sogou:cell:4.scel", IME: "sogou", Cell: true, Name: "4", Path: filepath.Join(sogou, "scd", "4.scel"), Words: 1},
		{ID: "squirrel:luna_pinyin", IME: "squirrel", Name: "luna_pinyin", Path: filepath.Join(squirrel, "luna_pinyin.userdb"), Rime: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("macOS sources:\n got %+v\nwant %+v", got, want)
	}
	got = FindSources(Env{GOOS: "windows", Home: home, AppData: appData})
	if len(got) != 1 || got[0].ID != "weasel:rime_ice" || !got[0].Rime {
		t.Fatalf("Windows sources: %+v", got)
	}
	// Shixin's own Rime folder is never a source.
	if got := FindSources(Env{GOOS: "windows", AppData: appData, Own: filepath.Join(appData, "Rime")}); len(got) != 0 {
		t.Fatalf("offered Shixin's own dictionary: %+v", got)
	}
	if got := FindSources(Env{GOOS: "darwin", Home: t.TempDir()}); len(got) != 0 {
		t.Fatalf("nothing installed: %+v", got)
	}
}

func TestRimeSourceName(t *testing.T) {
	for path, want := range map[string]string{
		"/x/Rime/rime_ice.userdb":     "rime_ice",
		"/x/Rime/luna_pinyin.userdb":  "luna_pinyin",
		"/x/Rime/wubi86.userdb":       "",
		"/x/Rime/rime_ice":            "",
		"/x/Rime/rime_ice.userdb.txt": "",
	} {
		if got := RimeSourceName(path); got != want {
			t.Errorf("%s: %q", path, got)
		}
	}
}
