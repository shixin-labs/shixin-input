package policy

import (
	"reflect"
	"testing"
)

func sample() *Policy {
	p := &Policy{
		ID:             7,
		Composing:      8,
		Notify:         NotifyShift,
		SpecialDefault: Act(Ask, Unknown),
		CommandDefault: Act(Pass, Same),
		Special: []Special{
			{Keysym: 0xff0d, Action: Act(Eat, Unknown)},              // Return
			{Keysym: 0xff0d, Shift: true, Action: Act(Ask, Unknown)}, // Shift+Return
			{Keysym: 0xff08, Action: Act(Eat, Unknown)},              // BackSpace
		},
		Hotkeys: []Hotkey{
			{Keysym: '`', Mods: ControlMask | ShiftMask},
			{Keysym: '`', Mods: ControlMask},
		},
	}
	for c := 'a'; c <= 'z'; c++ {
		p.Printable[c-0x20] = Act(Eat, Composing)
	}
	p.Printable[','-0x20] = Act(Eat, Same)
	p.Sort()
	return p
}

func TestLookup(t *testing.T) {
	p := sample()
	for _, c := range []struct {
		keysym, mods uint32
		want         Action
	}{
		{'a', 0, Act(Eat, Composing)},
		{',', 0, Act(Eat, Same)},
		{'1', 0, Act(Pass, Same)},
		{'a', ControlMask, Act(Pass, Same)},
		{'`', ControlMask, Act(Ask, Unknown)},
		{'`', ControlMask | ShiftMask, Act(Ask, Unknown)},
		{'`', ControlMask | LockMask, Act(Ask, Unknown)}, // Caps Lock doesn't matter
		{'`', AltMask, Act(Pass, Same)},
		{0xff0d, 0, Act(Eat, Unknown)},
		{0xff0d, ShiftMask, Act(Ask, Unknown)},
		{0xff08, 0, Act(Eat, Unknown)},
		{0xff1b, 0, Act(Ask, Unknown)}, // SpecialDefault
		{KeyShiftL, 0, Act(PassNotify, Same)},
		{KeyShiftR, ReleaseMask, Act(PassNotify, Same)},
		{KeyControlL, 0, Act(Pass, Same)},
		{'a', ReleaseMask, Act(Pass, Same)},
	} {
		if got := p.Lookup(c.keysym, c.mods); got != c.want {
			t.Errorf("Lookup(%#x, %#x) = %v/%v, want %v/%v", c.keysym, c.mods,
				got.Disposition(), got.Next(), c.want.Disposition(), c.want.Next())
		}
	}
}

func TestPassthrough(t *testing.T) {
	for _, k := range []uint32{'a', ',', 0xff0d, KeyShiftL} {
		if d := Passthrough.Lookup(k, 0).Disposition(); d != Pass {
			t.Errorf("Passthrough.Lookup(%#x) = %v", k, d)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	p := sample()
	b := p.Append([]byte{0xee})
	var q Policy
	rest, err := q.Decode(b[1:])
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("%d trailing bytes", len(rest))
	}
	if !reflect.DeepEqual(p, &q) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", q, *p)
	}
	for i := range b[1:] {
		if _, err := q.Decode(b[1 : 1+i]); err == nil {
			t.Fatalf("Decode of %d-byte prefix succeeded", i)
		}
	}
}

func TestLookupDoesNotAllocate(t *testing.T) {
	p := sample()
	var sink Action
	n := testing.AllocsPerRun(1000, func() {
		sink ^= p.Lookup('a', 0)
		sink ^= p.Lookup(0xff0d, ShiftMask)
		sink ^= p.Lookup('`', ControlMask)
	})
	if n != 0 {
		t.Fatalf("Lookup allocates %v times", n)
	}
}

func TestDecodeReusesSlices(t *testing.T) {
	b := sample().Append(nil)
	var q Policy
	if _, err := q.Decode(b); err != nil {
		t.Fatal(err)
	}
	n := testing.AllocsPerRun(100, func() {
		if _, err := q.Decode(b); err != nil {
			t.Fatal(err)
		}
	})
	if n != 0 {
		t.Fatalf("Decode allocates %v times", n)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(sample().Append(nil))
	f.Fuzz(func(t *testing.T, b []byte) {
		var p Policy
		if _, err := p.Decode(b); err != nil {
			return
		}
		// Whatever decodes must be usable.
		p.Lookup('a', 0)
		p.Lookup(0xff0d, ShiftMask)
		p.Lookup('`', ControlMask)
	})
}
