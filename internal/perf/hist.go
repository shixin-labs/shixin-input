// Package perf measures latency on the key path without allocating
// (DESIGN R8, §9).
package perf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"time"
)

// Hist is a log-linear latency histogram with about 12% resolution from
// 1 µs to about 70 s. The zero value is ready; Add doesn't allocate.
type Hist struct {
	n       uint64
	max     time.Duration
	buckets [nBuckets]uint32
}

const (
	subBits  = 3 // 8 sub-buckets per power of two
	nBuckets = 27 << subBits
)

func bucket(us uint64) int {
	if us < 1<<subBits {
		return int(us)
	}
	e := bits.Len64(us) - 1 // us in [2^e, 2^(e+1))
	sub := (us >> (e - subBits)) & (1<<subBits - 1)
	i := (e-subBits+1)<<subBits + int(sub)
	return min(i, nBuckets-1)
}

// lower returns the smallest µs value of bucket i.
func lower(i int) uint64 {
	if i < 1<<subBits {
		return uint64(i)
	}
	e := i>>subBits + subBits - 1
	sub := uint64(i & (1<<subBits - 1))
	return 1<<e | sub<<(e-subBits)
}

// Add records d.
func (h *Hist) Add(d time.Duration) {
	if d < 0 {
		d = 0
	}
	h.buckets[bucket(uint64(d/time.Microsecond))]++
	h.n++
	if d > h.max {
		h.max = d
	}
}

// Count returns the number of samples.
func (h *Hist) Count() uint64 { return h.n }

// Max returns the largest sample.
func (h *Hist) Max() time.Duration { return h.max }

// Quantile returns an upper bound of the q-quantile (0 < q ≤ 1).
func (h *Hist) Quantile(q float64) time.Duration {
	if h.n == 0 {
		return 0
	}
	rank := uint64(q*float64(h.n) + 0.5)
	rank = max(1, min(rank, h.n))
	var seen uint64
	for i, c := range h.buckets {
		seen += uint64(c)
		if seen >= rank {
			if i+1 < nBuckets {
				return min(time.Duration(lower(i+1))*time.Microsecond, h.max)
			}
			return h.max
		}
	}
	return h.max
}

// String summarizes the histogram as "p50 … p99 … max … (n)".
func (h *Hist) String() string {
	r := func(d time.Duration) time.Duration { return d.Round(time.Microsecond) }
	return fmt.Sprintf("p50 %v p99 %v max %v (n=%d)", r(h.Quantile(.5)), r(h.Quantile(.99)), r(h.max), h.n)
}

// Append appends h in a compact binary form (for the engine's stats
// message): u64 count | u64 max µs | u16 n | n × (u16 bucket, u32 count),
// non-empty buckets only, little-endian.
func (h *Hist) Append(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, h.n)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(h.max/time.Microsecond))
	n := 0
	for _, c := range h.buckets {
		if c != 0 {
			n++
		}
	}
	dst = binary.LittleEndian.AppendUint16(dst, uint16(n))
	for i, c := range h.buckets {
		if c != 0 {
			dst = binary.LittleEndian.AppendUint16(dst, uint16(i))
			dst = binary.LittleEndian.AppendUint32(dst, c)
		}
	}
	return dst
}

// ErrMalformed reports a histogram that doesn't decode.
var ErrMalformed = errors.New("perf: malformed histogram")

// Decode reads a histogram written by Append into h and returns the rest
// of b.
func (h *Hist) Decode(b []byte) ([]byte, error) {
	*h = Hist{}
	if len(b) < 18 {
		return nil, ErrMalformed
	}
	h.n = binary.LittleEndian.Uint64(b)
	h.max = time.Duration(binary.LittleEndian.Uint64(b[8:])) * time.Microsecond
	n := int(binary.LittleEndian.Uint16(b[16:]))
	b = b[18:]
	if len(b) < 6*n {
		return nil, ErrMalformed
	}
	for range n {
		i := int(binary.LittleEndian.Uint16(b))
		if i >= nBuckets {
			return nil, ErrMalformed
		}
		h.buckets[i] = binary.LittleEndian.Uint32(b[2:])
		b = b[6:]
	}
	return b, nil
}

// Bucket is one histogram bucket: samples from Lo up to the next bucket's.
type Bucket struct {
	Lo    time.Duration
	Count uint32
}

// Buckets returns the non-empty buckets in order, for drawing.
func (h *Hist) Buckets() []Bucket {
	var out []Bucket
	for i, c := range h.buckets {
		if c != 0 {
			out = append(out, Bucket{Lo: time.Duration(lower(i)) * time.Microsecond, Count: c})
		}
	}
	return out
}
