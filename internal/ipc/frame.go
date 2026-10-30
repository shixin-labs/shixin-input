package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Reader splits a byte stream into frames, reusing one buffer.
type Reader struct {
	r      io.Reader
	buf    []byte
	lo, hi int
}

// NewReader returns a Reader with an initial buffer of size bytes. The
// buffer grows up to MaxFrame for large frames.
func NewReader(r io.Reader, size int) *Reader {
	return &Reader{r: r, buf: make([]byte, max(size, 64))}
}

// Next returns the next frame's type and payload. The payload is valid
// until the following call.
func (fr *Reader) Next() (Type, []byte, error) {
	for {
		if n := fr.hi - fr.lo; n >= 4 {
			l := int(binary.LittleEndian.Uint32(fr.buf[fr.lo:]))
			if l < 1 || l > MaxFrame {
				return 0, nil, fmt.Errorf("ipc: bad frame length %d", l)
			}
			if n >= 4+l {
				f := fr.buf[fr.lo+4 : fr.lo+4+l : fr.lo+4+l]
				fr.lo += 4 + l
				return Type(f[0]), f[1:], nil
			}
			if 4+l > len(fr.buf) {
				nb := make([]byte, max(4+l, 2*len(fr.buf)))
				fr.hi = copy(nb, fr.buf[fr.lo:fr.hi])
				fr.lo, fr.buf = 0, nb
			}
		}
		if fr.lo > 0 && (fr.hi == len(fr.buf) || fr.lo == fr.hi) {
			fr.hi = copy(fr.buf, fr.buf[fr.lo:fr.hi])
			fr.lo = 0
		}
		n, err := fr.r.Read(fr.buf[fr.hi:])
		fr.hi += n
		if err != nil {
			if n > 0 {
				continue
			}
			if err == io.EOF && fr.hi > fr.lo {
				err = io.ErrUnexpectedEOF
			}
			return 0, nil, err
		}
	}
}
