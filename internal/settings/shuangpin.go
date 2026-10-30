package settings

import "strings"

// ShuangpinKey describes a physical key and its pinyin initials and finals.
type ShuangpinKey struct {
	Key, Initial string
	Finals       []string
}

// ShuangpinSpelling is a syllable without an initial and its accepted codes.
type ShuangpinSpelling struct {
	Pinyin string
	Codes  []string
}

// ShuangpinLayout is the reference chart for a supported double-pinyin scheme.
type ShuangpinLayout struct {
	Keys         []ShuangpinKey
	ZeroInitials []ShuangpinSpelling
}

// Shuangpin returns read-only reference data, or nil for full pinyin or an
// unknown scheme. The chart describes the base layout, before fuzzy rules.
func Shuangpin(scheme string) *ShuangpinLayout { return shuangpinLayouts[scheme] }

// These mappings follow the bundled rime-ice double_pinyin*.schema.yaml
// configurations (fetched by scripts/fetch-rime-data.sh). Engine tests apply
// their actual spelling algebra to a small dictionary and check every entry.
var shuangpinLayouts = map[string]*ShuangpinLayout{
	"double_pinyin": newShuangpin("v", "i", "u", `
		q:iu w:ia/ua e:e r:uan/er t:üe y:ing/uai u:u i:i o:o/uo p:un
		a:a s:ong/iong d:iang/uang f:en g:eng h:ang j:an k:ao l:ai
		z:ei x:ie c:iao v:ü/ui b:ou n:in m:ian`,
		`a:aa ai:ai/al ang:ah e:ee er:er o:oo`),
	"double_pinyin_flypy": newShuangpin("v", "i", "u", `
		q:iu w:ei e:e r:uan/er t:üe y:un u:u i:i o:o/uo p:ie
		a:a s:ong/iong d:ai f:en g:eng h:ang j:an k:ing/uai l:iang/uang
		z:ou x:ia/ua c:ao v:ü/ui b:in n:iao m:ian`,
		`a:aa ai:ai/ad ang:ah e:ee er:er o:oo`),
	"double_pinyin_mspy": newShuangpin("v", "i", "u", `
		q:iu w:ia/ua e:e r:uan/er t:üe y:ü/uai u:u i:i o:o/uo p:un
		a:a s:ong/iong d:iang/uang f:en g:eng h:ang j:an k:ao l:ai ;:ing
		z:ei x:ie c:iao v:ui/üe b:ou n:in m:ian`,
		`a:aa/oa ai:al/ol ang:ah/oh e:ee/oe er:er/or o:oo`),
	"double_pinyin_sogou": newShuangpin("v", "i", "u", `
		q:iu w:ia/ua e:e r:uan/er t:üe y:ü/uai u:u i:i o:o/uo p:un
		a:a s:ong/iong d:iang/uang f:en g:eng h:ang j:an k:ao l:ai ;:ing
		z:ei x:ie c:iao v:ui b:ou n:in m:ian`,
		`a:aa/oa ai:al/ol ang:ah/oh e:ee/oe er:er/or o:oo`),
	"double_pinyin_abc": newShuangpin("a", "e", "v", `
		q:ei w:ian e:e r:iu/er t:iang/uang y:ing u:u i:i o:o/uo p:uan
		a:a s:ong/iong d:ia/ua f:en g:eng h:ang j:an k:ao l:ai
		z:iao x:ie c:in/uai v:ü b:ou n:un m:üe/ui`,
		`a:oa ai:ol ang:oh e:oe er:or o:oo`),
	"double_pinyin_ziguang": newShuangpin("u", "a", "i", `
		q:ao w:en e:e r:an t:eng y:in/uai u:u i:i o:o/uo p:ai
		a:a s:ang d:ie f:ian g:iang/uang h:ong/iong j:iu/er k:ei l:uan ;:ing
		z:ou x:ia/ua v:ü b:iao n:üe/ui m:un`,
		`a:oa ai:op ang:os e:oe er:oj o:oo`),
	"double_pinyin_jiajia": newShuangpin("v", "u", "i", `
		q:ing/er w:ei e:e r:en t:eng y:ong/iong u:u i:i o:o/uo p:ou
		a:a s:ai d:ao f:an g:ang h:iang/uang j:ian k:iao l:in
		z:un x:üe/uai c:uan v:ü/ui b:ia/ua n:iu m:ie`,
		`a:aa/oa ai:as/os ang:ag/og e:ee/oe er:eq/oq o:oo`),
}

func newShuangpin(zh, ch, sh, finals, zero string) *ShuangpinLayout {
	l := &ShuangpinLayout{}
	byKey := map[string][]string{}
	for _, field := range strings.Fields(finals) {
		key, sounds, _ := strings.Cut(field, ":")
		byKey[key] = strings.Split(sounds, "/")
	}
	for _, key := range strings.Split("qwertyuiopasdfghjkl;zxcvbnm", "") {
		initial := ""
		if strings.Contains("bpmfdtnlgkhjqxrzcsyw", key) {
			initial = key
		}
		switch key {
		case zh:
			initial = "zh"
		case ch:
			initial = "ch"
		case sh:
			initial = "sh"
		}
		l.Keys = append(l.Keys, ShuangpinKey{key, initial, byKey[key]})
	}
	for _, field := range strings.Fields(zero) {
		pinyin, codes, _ := strings.Cut(field, ":")
		l.ZeroInitials = append(l.ZeroInitials, ShuangpinSpelling{pinyin, strings.Split(codes, "/")})
	}
	return l
}
