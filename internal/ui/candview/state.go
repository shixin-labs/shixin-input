package candview

import (
	"strconv"

	"shixin/internal/ipc"
)

const (
	stringCacheEntries = 512
	stringCacheBytes   = 256 << 10
)

// stringCache owns immutable copies: IPC buffers are reused as soon as Drain
// returns. Neither a byte-to-string alias nor a mutable string is safe for
// MyGo's text/layout caches. Bound both the entry count and retained bytes.
type stringCache struct {
	values map[string]string
	order  [stringCacheEntries]string
	head   int
	count  int
	bytes  int
}

func (c *stringCache) intern(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if s, ok := c.values[string(b)]; ok { // lookup converts without allocating
		return s
	}
	s := string(b)
	if len(s) > stringCacheBytes {
		return s // display long text without retaining it in the cache
	}
	if c.values == nil {
		c.values = make(map[string]string, stringCacheEntries)
	}
	for c.count == len(c.order) || c.bytes+len(s) > stringCacheBytes {
		old := c.order[c.head]
		delete(c.values, old)
		c.bytes -= len(old)
		c.order[c.head] = ""
		c.head = (c.head + 1) % len(c.order)
		c.count--
	}
	c.order[(c.head+c.count)%len(c.order)] = s
	c.count++
	c.bytes += len(s)
	c.values[s] = s
	return s
}

// Apply updates display strings on the UI frame. Reuse unchanged strings in
// place and intern changes across candidate positions and successive pages.
// New text allocates once on a cache miss, outside the key callback.
func (st *State) Apply(r *ipc.Result) {
	if st.strings == nil {
		st.strings = new(stringCache)
	}
	if st.Preedit != string(r.Preedit) {
		st.Preedit = st.strings.intern(r.Preedit)
	}
	n := len(r.Candidates)
	if n < len(st.Items) {
		clear(st.Items[n:])
	}
	for len(st.Items) < n {
		i := len(st.Items)
		label := ""
		if i < len(Labels) {
			label = Labels[i]
		} else {
			label = strconv.Itoa(i + 1)
		}
		st.Items = append(st.Items, Item{Label: label})
	}
	st.Items = st.Items[:n]
	for i := range r.Candidates {
		it, src := &st.Items[i], &r.Candidates[i]
		if it.Text != string(src.Text) {
			it.Text = st.strings.intern(src.Text)
		}
		if it.Comment != string(src.Comment) {
			it.Comment = st.strings.intern(src.Comment)
		}
	}
	st.Highlight = int(r.Highlighted)
}

// Clear hides the content while retaining buffers, handlers and bounded text
// caches for the next composition. No data is written to disk or logged.
func (st *State) Clear() {
	st.Preedit, st.Highlight = "", 0
	clear(st.Items)
	st.Items = st.Items[:0]
}

func (st *State) selectionHandler(i int) func() {
	for len(st.clicks) <= i {
		index := len(st.clicks)
		st.clicks = append(st.clicks, func() {
			if st.onSelect != nil {
				st.onSelect(index)
			}
		})
	}
	return st.clicks[i]
}
