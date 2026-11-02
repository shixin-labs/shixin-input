// Package dictionary reads portable, offline dictionary files. It never opens
// Rime databases; only the coordinator's exclusive maintenance child may do so.
package dictionary

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"shixin/internal/settings"
)

const MaxBytes = 64 << 20
const MaxEntries = 250000

// ErrEmpty distinguishes an empty export from an invalid record.
var ErrEmpty = errors.New("dictionary: no entries")

// Entry uses full-pinyin syllables separated by spaces, in every input layout.
type Entry struct {
	Text  string `json:"text"`
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// Backup includes learned words, custom phrases and persistent candidate pins.
// It deliberately excludes preferences and app identifiers.
type Backup struct {
	Format  string            `json:"format"`
	Version int               `json:"version"`
	Words   []Entry           `json:"words"`
	Phrases []settings.Phrase `json:"phrases"`
	Pins    []settings.Pin    `json:"pins"`
}

func validEntry(e Entry) bool {
	if e.Text == "" || len(e.Text) > 4096 || !utf8.ValidString(e.Text) || strings.HasPrefix(e.Text, "#") || strings.ContainsFunc(e.Text, unicode.IsControl) || e.Count < 0 || e.Count > 1000000000 || len(e.Code) > 512 {
		return false
	}
	parts := strings.Fields(e.Code)
	if len(parts) == 0 || strings.Join(parts, " ") != e.Code {
		return false
	}
	for _, p := range parts {
		if len(p) > 8 {
			return false
		}
		for _, c := range p {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	return true
}

// Read accepts Shixin JSON backups, Rime/Sogou tab-separated text and standard
// Sogou .scel files. Malformed input fails as a whole, without partial imports.
func Read(r io.Reader) (Backup, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Backup{}, errors.New("dictionary: cannot read file")
	}
	if len(b) > MaxBytes {
		return Backup{}, errors.New("dictionary: file exceeds 64 MiB")
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	out := Backup{Format: "shixin-dictionary", Version: 1}
	portable := false
	switch {
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{0x40, 0x15, 0, 0}):
		out.Words, err = readSCEL(b)
	case bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")):
		portable = true
		if !utf8.Valid(b) {
			return Backup{}, errors.New("dictionary: backup must be UTF-8")
		}
		out = Backup{}
		err = json.Unmarshal(b, &out)
		if err != nil || out.Format != "shixin-dictionary" || out.Version != 1 {
			return Backup{}, errors.New("dictionary: unsupported backup")
		}
	default:
		out.Words, err = readText(b)
	}
	if err != nil {
		return Backup{}, err
	}
	if len(out.Words) > MaxEntries {
		return Backup{}, errors.New("dictionary: too many entries")
	}
	for i, e := range out.Words {
		if !validEntry(e) {
			return Backup{}, fmt.Errorf("dictionary: invalid entry %d", i+1)
		}
	}
	s := settings.Default()
	s.Phrases, s.Pins = out.Phrases, out.Pins
	if err := s.Validate(); err != nil {
		return Backup{}, errors.New("dictionary: invalid custom phrases or pins")
	}
	if !portable && len(out.Words)+len(out.Phrases)+len(out.Pins) == 0 {
		return Backup{}, ErrEmpty
	}
	return out, nil
}

func ReadFile(path string) (Backup, error) {
	f, err := os.Open(path)
	if err != nil {
		return Backup{}, errors.New("dictionary: cannot open file")
	}
	defer f.Close()
	return Read(f)
}

func readText(b []byte) ([]Entry, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("dictionary: text must be UTF-8")
	}
	var out []Entry
	scan := bufio.NewScanner(bytes.NewReader(b))
	scan.Buffer(make([]byte, 4096), 8192)
	header := false
	for line := 1; scan.Scan(); line++ {
		s := strings.TrimSpace(scan.Text())
		if s == "---" {
			header = true
			continue
		}
		if header {
			if s == "..." {
				header = false
			}
			continue
		}
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		parts := strings.Split(s, "\t")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("dictionary: expected tab-separated text and full pinyin at line %d", line)
		}
		e := Entry{Text: strings.TrimSpace(parts[0]), Code: strings.TrimSpace(parts[1]), Count: 1}
		// Sogou's plain-text exports may put pinyin before the word.
		if asciiCode(e.Text) && !asciiCode(e.Code) {
			e.Text, e.Code = e.Code, e.Text
		}
		e.Code = strings.Join(strings.Fields(strings.NewReplacer("ü", "v", "u:", "v", "'", " ").Replace(strings.ToLower(e.Code))), " ")
		if len(parts) == 3 {
			count := parts[2]
			// Rime sync snapshots use code, text and packed c=/d=/t= fields.
			if strings.HasPrefix(count, "c=") {
				count = strings.TrimPrefix(strings.Fields(count)[0], "c=")
				if n, err := strconv.Atoi(count); err == nil && n < 0 {
					continue
				} // deletion tombstone
			}
			var err error
			e.Count, err = strconv.Atoi(count)
			if err != nil {
				return nil, fmt.Errorf("dictionary: invalid count at line %d", line)
			}
		}
		if !validEntry(e) {
			return nil, fmt.Errorf("dictionary: invalid entry at line %d", line)
		}
		out = append(out, e)
		if len(out) > MaxEntries {
			return nil, errors.New("dictionary: too many entries")
		}
	}
	if scan.Err() != nil || header {
		return nil, errors.New("dictionary: truncated or oversized text record")
	}
	return out, nil
}

func asciiCode(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != ' ' && r != '\'' && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func utf16Text(b []byte) (string, error) {
	if len(b)%2 != 0 {
		return "", errors.New("dictionary: invalid UTF-16 record")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	for i := 0; i < len(u); i++ {
		if u[i] >= 0xd800 && u[i] <= 0xdbff {
			if i+1 >= len(u) || u[i+1] < 0xdc00 || u[i+1] > 0xdfff {
				return "", errors.New("dictionary: invalid surrogate")
			}
			i++
		} else if u[i] >= 0xdc00 && u[i] <= 0xdfff {
			return "", errors.New("dictionary: invalid surrogate")
		}
	}
	return string(utf16.Decode(u)), nil
}

// SCEL stores a UTF-16 syllable table at 0x1540 and grouped word records.
// Only the two standard container versions are accepted; no guessing offsets.
func readSCEL(b []byte) ([]Entry, error) {
	bad := errors.New("dictionary: malformed or unsupported SCEL file")
	if len(b) < 0x1544 {
		return nil, bad
	}
	wordOffset := 0
	switch b[4] {
	case 0x44:
		wordOffset = 0x2628
	case 0x45:
		wordOffset = 0x26c4
	default:
		return nil, bad
	}
	if len(b) < wordOffset {
		return nil, bad
	}
	py := map[uint16]string{}
	for p := 0x1544; p+4 <= wordOffset; {
		id, n := binary.LittleEndian.Uint16(b[p:]), int(binary.LittleEndian.Uint16(b[p+2:]))
		p += 4
		if n == 0 {
			break
		}
		if n > 16 || p+n > wordOffset {
			return nil, bad
		}
		s, err := utf16Text(b[p : p+n])
		s = strings.ReplaceAll(s, "ü", "v")
		if err != nil || !asciiCode(s) {
			return nil, bad
		}
		if _, duplicate := py[id]; duplicate {
			return nil, bad
		}
		py[id] = s
		p += n
	}
	var out []Entry
	for p := wordOffset; p < len(b); {
		if p+4 > len(b) {
			return nil, bad
		}
		n, codeN := int(binary.LittleEndian.Uint16(b[p:])), int(binary.LittleEndian.Uint16(b[p+2:]))
		p += 4
		if n == 0 || codeN == 0 || codeN%2 != 0 || codeN > 256 || p+codeN > len(b) {
			return nil, bad
		}
		parts := make([]string, codeN/2)
		for i := range parts {
			parts[i] = py[binary.LittleEndian.Uint16(b[p+2*i:])]
			if parts[i] == "" {
				return nil, bad
			}
		}
		p += codeN
		code := strings.Join(parts, " ")
		for range n {
			if p+2 > len(b) {
				return nil, bad
			}
			textN := int(binary.LittleEndian.Uint16(b[p:]))
			p += 2
			if textN == 0 || textN > 4096 || p+textN+2 > len(b) {
				return nil, bad
			}
			text, err := utf16Text(b[p : p+textN])
			if err != nil {
				return nil, bad
			}
			p += textN
			extN := int(binary.LittleEndian.Uint16(b[p:]))
			p += 2
			if extN < 2 || p+extN > len(b) {
				return nil, bad
			}
			count := int(binary.LittleEndian.Uint16(b[p:]))
			p += extN
			out = append(out, Entry{Text: text, Code: code, Count: max(1, count)})
			if len(out) > MaxEntries {
				return nil, errors.New("dictionary: too many entries")
			}
		}
	}
	return out, nil
}

// WriteTable writes the interoperable Rime text format, never YAML or code.
func WriteTable(w io.Writer, entries []Entry) error {
	b := bufio.NewWriter(w)
	for _, e := range entries {
		if !validEntry(e) {
			return errors.New("dictionary: invalid entry")
		}
		if _, err := fmt.Fprintf(b, "%s\t%s\t%d\n", e.Text, e.Code, e.Count); err != nil {
			return err
		}
	}
	return b.Flush()
}

// WriteBackup writes a portable backup. The caller owns atomic publication.
func WriteBackup(w io.Writer, b Backup) error {
	b.Format, b.Version = "shixin-dictionary", 1
	enc := json.NewEncoder(&limitedWriter{w: w, left: MaxBytes})
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

type limitedWriter struct {
	w    io.Writer
	left int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > w.left {
		return 0, errors.New("dictionary: backup exceeds 64 MiB")
	}
	n, err := w.w.Write(p)
	w.left -= n
	return n, err
}
