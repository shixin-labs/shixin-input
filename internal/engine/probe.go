package engine

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"shixin/internal/policy"
	"shixin/internal/rime"
)

// Policy building (DESIGN §3.3). EAT must be certain, so instead of
// re-implementing librime's processors, the engine asks librime: at warm-up
// it feeds every key to a probe session from each state it builds a policy
// for, and records whether librime handled it.
//
// Probes never select a candidate, so they never write the user
// dictionary. Keys whose outcome depends on more than the state the policy
// is keyed by (caret position, the menu, key_binder conditions, hotkeys that
// open the switcher) are not probed and become ASK.

// schemaPolicies holds policy IDs for one schema, indexed by ascii_punct.
type schemaPolicies struct {
	chinese, ascii, composing [2]uint16
}

// conservativeID is the policy used for states without a probed policy,
// e.g. full-width mode: every key that might matter is ASK.
const conservativeID = 0

func conservativePolicy() policy.Policy {
	ask := policy.Act(policy.Ask, policy.Unknown)
	p := policy.Policy{
		ID:             conservativeID,
		SpecialDefault: ask,
		CommandDefault: ask,
		Notify:         policy.NotifyShift | policy.NotifyControl,
	}
	for i := range p.Printable {
		p.Printable[i] = ask
	}
	return p
}

// probeKeys are the non-printable keys probed in the idle state, each with
// and without Shift. Other non-printable keys are not handled by librime's
// processors when idle unless a binding names them.
var probeKeys = []uint32{
	policy.KeyBackSpace, policy.KeyTab, policy.KeyReturn, policy.KeyEscape, policy.KeyDelete, policy.KeyHome, policy.KeyEnd,
	policy.KeyLeft, policy.KeyRight, policy.KeyUp, policy.KeyDown, policy.KeyPrior, policy.KeyNext, policy.KeyKPEnter,
}

// composingKeys are the non-printable keys that may be EAT while composing:
// the editor's bindings, whose actions always succeed. They are verified by
// probing; every other non-printable key is ASK while composing because
// navigator/selector actions fail at the edges (Left at the start, Up on
// the first candidate) and then pass the key to the app.
var composingKeys = []uint32{policy.KeyBackSpace, policy.KeyReturn, policy.KeyEscape}

type schemaConfig struct {
	alphabet   string
	initial    byte // a letter that starts a composition, for probing
	autoSelect bool // a letter can end the composition
	notify     uint8
	hotkeys    []policy.Hotkey // command-modifier bindings
	bound      map[uint32]bool // non-command keys with bindings; never probed
	// digitSeps are punctuator/digit_separators: after a digit, librime
	// keeps them pending (3.5, 12:30) instead of committing 。 or ：.
	digitSeps string
}

func readSchemaConfig(lib *rime.Lib, schema string) (*schemaConfig, error) {
	cfg, err := lib.OpenSchema(schema)
	if err != nil {
		return nil, err
	}
	defer cfg.Close()
	// DESIGN R9: no Lua, anywhere. The settings compiler writes Lua-free
	// engine lists; refuse to serve a schema that has any lua_* component.
	for _, list := range []string{"processors", "segmentors", "translators", "filters"} {
		key := "engine/" + list
		for i, n := 0, cfg.ListSize(key); i < n; i++ {
			if c, _ := cfg.String(key + "/@" + strconv.Itoa(i)); strings.HasPrefix(c, "lua_") {
				return nil, fmt.Errorf("schema %s: %s lists %s; Lua is not allowed (DESIGN R9)", schema, key, c)
			}
		}
	}
	sc := &schemaConfig{bound: map[uint32]bool{}}
	sc.alphabet, _ = cfg.String("speller/alphabet")
	if sc.alphabet == "" {
		sc.alphabet = "zyxwvutsrqponmlkjihgfedcba"
	}
	initials, _ := cfg.String("speller/initials")
	if initials == "" {
		initials = sc.alphabet
	}
	for _, c := range []byte("ainhsz" + initials) {
		if strings.IndexByte(initials, c) >= 0 && c >= 'a' && c <= 'z' {
			sc.initial = c
			break
		}
	}
	if sc.initial == 0 {
		return nil, fmt.Errorf("schema %s: no lowercase initial to probe with", schema)
	}
	auto, _ := cfg.Bool("speller/auto_select")
	_, maxLen := cfg.Int("speller/max_code_length")
	clear, _ := cfg.String("speller/auto_clear")
	sc.autoSelect = auto || maxLen || clear != ""
	var ok bool
	if sc.digitSeps, ok = cfg.String("punctuator/digit_separators"); !ok {
		sc.digitSeps = ".:" // librime's PunctConfig default
	}

	// ascii_composer falls back to default.yaml when the schema has no
	// switch_key map, as librime's AsciiComposer::LoadConfig does.
	def, err := lib.OpenConfig("default")
	if err != nil {
		return nil, err
	}
	defer def.Close()
	switchKeys := cfg
	if _, ok := cfg.String("ascii_composer/switch_key/Shift_L"); !ok {
		switchKeys = def
	}
	for _, k := range []struct {
		key  string
		flag uint8
	}{{"Shift_L", policy.NotifyShift}, {"Shift_R", policy.NotifyShift},
		{"Control_L", policy.NotifyControl}, {"Control_R", policy.NotifyControl}} {
		if v, ok := switchKeys.String("ascii_composer/switch_key/" + k.key); ok && v != "noop" {
			sc.notify |= k.flag
		}
	}

	bind := func(repr string) {
		keysym, mods, ok := parseKey(repr)
		if !ok {
			return // unknown names can't match a key we send
		}
		if mods&policy.CommandMask != 0 {
			sc.hotkeys = append(sc.hotkeys, policy.Hotkey{Keysym: keysym, Mods: mods &^ (policy.LockMask | policy.ReleaseMask)})
		} else {
			sc.bound[keysym] = true
		}
	}
	for i, n := 0, cfg.ListSize("switcher/hotkeys"); i < n; i++ {
		if s, ok := cfg.String("switcher/hotkeys/@" + strconv.Itoa(i)); ok {
			bind(s)
		}
	}
	// The idle policies only need bindings that can fire without a
	// composition: librime's other conditions (composing, has_menu, paging,
	// predicting) all need one, and the composing policy asks for every
	// key that isn't printable anyway.
	for i, n := 0, cfg.ListSize("key_binder/bindings"); i < n; i++ {
		p := "key_binder/bindings/@" + strconv.Itoa(i) + "/"
		if when, _ := cfg.String(p + "when"); when != "always" {
			continue
		}
		if s, ok := cfg.String(p + "accept"); ok {
			bind(s)
		}
	}
	// The switcher's hotkeys usually live in default.yaml.
	for i, n := 0, def.ListSize("switcher/hotkeys"); i < n; i++ {
		if s, ok := def.String("switcher/hotkeys/@" + strconv.Itoa(i)); ok {
			bind(s)
		}
	}
	return sc, nil
}

// prober feeds single keys to a scratch session.
type prober struct {
	s      *rime.Session
	st     rime.Status
	commit []byte
}

// key processes one key from the current state and reports whether it was
// handled and whether the session is composing afterwards. The session is
// reset to idle.
func (p *prober) key(keysym, mods uint32) (handled, composing bool) {
	handled = p.s.ProcessKey(int(keysym), int(mods))
	p.s.ReadStatus(&p.st)
	composing = p.st.IsComposing
	p.commit, _ = p.s.ReadCommit(p.commit)
	p.s.ClearComposition()
	return handled, composing
}

func (e *Engine) buildPolicies(p *prober, schema string) (*schemaPolicies, error) {
	if !p.s.SelectSchema(schema) {
		return nil, fmt.Errorf("select schema %s failed", schema)
	}
	savedPunct := p.s.Option("ascii_punct")
	defer p.s.SetOption("ascii_punct", savedPunct)
	sc, err := readSchemaConfig(e.lib, schema)
	if err != nil {
		return nil, err
	}
	ask := policy.Act(policy.Ask, policy.Unknown)
	idle := func(ascii bool) (uint16, error) {
		p.s.SetOption("ascii_mode", ascii)
		defer p.s.SetOption("ascii_mode", false)
		pol := policy.Policy{
			SpecialDefault: policy.Act(policy.Pass, policy.Same),
			CommandDefault: policy.Act(policy.Pass, policy.Same),
			Hotkeys:        sc.hotkeys,
			Notify:         sc.notify,
		}
		for c := byte(0x20); c < 0x7f; c++ {
			if sc.bound[uint32(c)] {
				pol.Printable[c-0x20] = ask
				continue
			}
			var mods uint32
			if shifted(c) {
				mods = policy.ShiftMask
			}
			a := probeAction(p.key(uint32(c), mods))
			if !ascii {
				a = afterDigits(sc, c, a)
			}
			pol.Printable[c-0x20] = a
		}
		for _, k := range probeKeys {
			for _, shift := range []bool{false, true} {
				a := ask
				if !sc.bound[k] {
					var mods uint32
					if shift {
						mods = policy.ShiftMask
					}
					a = probeAction(p.key(k, mods))
				}
				// 智能排序 hears about caret moves: what is typed next may
				// not continue the text before (DESIGN §7.2).
				if !ascii && e.rr.attached && caretKey(k) && a.Disposition() == policy.Pass {
					a = policy.Act(policy.PassNotify, policy.Same)
				}
				pol.Special = append(pol.Special, policy.Special{Keysym: k, Shift: shift, Action: a})
			}
		}
		for k := range sc.bound {
			if k > 0x7e && !slices.Contains(probeKeys, k) {
				pol.Special = append(pol.Special, policy.Special{Keysym: k, Action: ask},
					policy.Special{Keysym: k, Shift: true, Action: ask})
			}
		}
		if p.s.Option("ascii_mode") != ascii {
			return 0, fmt.Errorf("schema %s: probing changed ascii_mode", schema)
		}
		return e.addPolicy(pol), nil
	}

	sp := &schemaPolicies{}
	for punct := range 2 {
		p.s.SetOption("ascii_punct", punct != 0)
		if sp.chinese[punct], err = idle(false); err != nil {
			return nil, err
		}
		if sp.ascii[punct], err = idle(true); err != nil {
			return nil, err
		}

		// Composing: every printable key is EAT. If librime declines one (e.g.
		// the editor commits the composition and rejects the key), the client
		// inserts the character itself, which is equivalent (§3.3, mispredict
		// repair).
		pol := policy.Policy{
			SpecialDefault: ask,
			CommandDefault: ask,
			Hotkeys:        sc.hotkeys,
			Notify:         sc.notify,
		}
		letter := policy.Act(policy.Eat, policy.Composing)
		if sc.autoSelect {
			letter = policy.Act(policy.Eat, policy.Unknown)
		}
		for c := byte(0x20); c < 0x7f; c++ {
			if strings.IndexByte(sc.alphabet, c) >= 0 && !sc.bound[uint32(c)] {
				pol.Printable[c-0x20] = letter
			} else {
				pol.Printable[c-0x20] = policy.Act(policy.Eat, policy.Unknown)
			}
		}
		for _, k := range composingKeys {
			a := ask
			if !sc.bound[k] {
				p.s.ProcessKey(int(sc.initial), 0)
				p.s.ReadStatus(&p.st)
				if !p.st.IsComposing {
					return nil, fmt.Errorf("schema %s: %q doesn't start a composition", schema, sc.initial)
				}
				if handled, _ := p.key(k, 0); handled {
					a = policy.Act(policy.Eat, policy.Unknown)
				}
			}
			pol.Special = append(pol.Special, policy.Special{Keysym: k, Action: a})
		}
		sp.composing[punct] = e.addPolicy(pol)
		for _, id := range []uint16{sp.chinese[punct], sp.ascii[punct], sp.composing[punct]} {
			e.pols[id].Composing = sp.composing[punct]
		}
	}
	return sp, nil
}

// afterDigits adjusts an idle Chinese action for librime's digit
// separators, which depend on the commit history rather than the state:
// the engine must see the digits the app inserts (PASS_NOTIFY, so 3.5
// doesn't become 3。5), and a separator after a digit starts a composition
// instead of committing, so the next key's policy is unknown.
func afterDigits(sc *schemaConfig, c byte, a policy.Action) policy.Action {
	if sc.digitSeps == "" {
		return a
	}
	switch {
	case c >= '0' && c <= '9' && a.Disposition() == policy.Pass:
		return policy.Act(policy.PassNotify, policy.Same)
	case strings.IndexByte(sc.digitSeps, c) >= 0 && a.Disposition() == policy.Eat:
		return policy.Act(policy.Eat, policy.Unknown)
	}
	return a
}

func probeAction(handled, composing bool) policy.Action {
	switch {
	case !handled:
		return policy.Act(policy.Pass, policy.Same)
	case composing:
		return policy.Act(policy.Eat, policy.Composing)
	}
	return policy.Act(policy.Eat, policy.Same)
}

func (e *Engine) addPolicy(p policy.Policy) uint16 {
	p.ID = uint16(len(e.pols))
	p.Hotkeys = append([]policy.Hotkey(nil), p.Hotkeys...)
	p.Sort()
	e.pols = append(e.pols, p)
	return p.ID
}
