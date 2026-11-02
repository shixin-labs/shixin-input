package dictionary

import (
	"encoding/binary"
	"errors"
	"strings"
)

// sogouSyllables is Sogou's fixed syllable numbering. Its user dictionaries
// use it directly; standard .scel files carry the same table explicitly.
var sogouSyllables = strings.Fields(`
a ai an ang ao ba bai ban bang bao bei ben beng bi bian biao bie bin bing
bo bu ca cai can cang cao ce cen ceng cha chai chan chang chao che chen
cheng chi chong chou chu chua chuai chuan chuang chui chun chuo ci cong cou
cu cuan cui cun cuo da dai dan dang dao de dei den deng di dia dian diao
die ding diu dong dou du duan dui dun duo e ei en eng er fa fan fang fei
fen feng fiao fo fou fu ga gai gan gang gao ge gei gen geng gong gou gu gua
guai guan guang gui gun guo ha hai han hang hao he hei hen heng hong hou hu
hua huai huan huang hui hun huo ji jia jian jiang jiao jie jin jing jiong
jiu ju juan jue jun ka kai kan kang kao ke kei ken keng kong kou ku kua
kuai kuan kuang kui kun kuo la lai lan lang lao le lei leng li lia lian
liang liao lie lin ling liu lo long lou lu luan lue lun luo lv ma mai man
mang mao me mei men meng mi mian miao mie min ming miu mo mou mu na nai nan
nang nao ne nei nen neng ni nian niang niao nie nin ning niu nong nou nu
nuan nue nun nuo nv o ou pa pai pan pang pao pei pen peng pi pian piao pie
pin ping po pou pu qi qia qian qiang qiao qie qin qing qiong qiu qu quan
que qun ran rang rao re ren reng ri rong rou ru rua ruan rui run ruo sa sai
san sang sao se sen seng sha shai shan shang shao she shei shen sheng shi
shou shu shua shuai shuan shuang shui shun shuo si song sou su suan sui sun
suo ta tai tan tang tao te tei teng ti tian tiao tie ting tong tou tu tuan
tui tun tuo wa wai wan wang wei wen weng wo wu xi xia xian xiang xiao xie
xin xing xiong xiu xu xuan xue xun ya yan yang yao ye yi yin ying yo yong
you yu yuan yue yun za zai zan zang zao ze zei zen zeng zha zhai zhan zhang
zhao zhe zhei zhen zheng zhi zhong zhou zhu zhua zhuai zhuan zhuang zhui
zhun zhuo zi zong zou zu zuan zui zun zuo`)

// readSogouUser reads Sogou for Mac's user dictionary (sgim_gd_usr.bin) and
// the backups its settings export, both starting with "SGPU". The header
// gives an index of uint32 offsets into a data area; each record is
//
//	freq u16 | time u32 | flags [3]byte | code bytes u16 | codes u16... |
//	ext bytes u16 | text bytes u16 | text UTF-16LE | typed codes...
//
// where ext bytes cover the text length, the text and the trailing codes.
// A broken structure fails the whole file. Records that are not Chinese
// words with full-pinyin codes (Sogou numbers abbreviations from 842) and
// forgotten words (frequency 0) are skipped: a user dictionary holds them
// normally.
func readSogouUser(b []byte) ([]Entry, error) {
	bad := errors.New("dictionary: malformed or unsupported Sogou user dictionary")
	if len(b) < 0x58 || string(b[:4]) != "SGPU" {
		return nil, bad
	}
	u16 := func(p int) int { return int(binary.LittleEndian.Uint16(b[p:])) }
	u32 := func(p int) int { return int(binary.LittleEndian.Uint32(b[p:])) }
	if u32(0x10) != len(b) {
		// Sogou may be rewriting the file; read it again later.
		return nil, bad
	}
	index, capacity, count := u32(0x38), u32(0x3c), u32(0x40)
	data, dataCap, used := u32(0x44), u32(0x48), u32(0x4c)
	// The index capacity is in bytes: four per record.
	if index < 0x58 || capacity > len(b) || index+capacity > data || 4*count > capacity ||
		dataCap > len(b) || data+dataCap > len(b) || used > dataCap {
		return nil, bad
	}
	if count > MaxEntries {
		return nil, errors.New("dictionary: too many entries")
	}
	end := data + used
	var out []Entry
	for i := range count {
		p := data + u32(index+4*i)
		if p+11 > end {
			return nil, bad
		}
		freq, codeN := u16(p), u16(p+9)
		p += 11
		if codeN == 0 || codeN%2 != 0 || codeN > 256 || p+codeN+4 > end {
			return nil, bad
		}
		parts := make([]string, codeN/2)
		for j := range parts {
			if id := u16(p + 2*j); id < len(sogouSyllables) {
				parts[j] = sogouSyllables[id]
			}
		}
		p += codeN
		extN, textN := u16(p), u16(p+2)
		p += 4
		if textN == 0 || textN > 4096 || extN < 2+textN || p-2+extN > end {
			return nil, bad
		}
		text, err := utf16Text(b[p : p+textN])
		if err != nil {
			return nil, bad
		}
		e := Entry{Text: text, Code: strings.Join(parts, " "), Count: min(freq, 1000000000)}
		// An unknown syllable leaves an empty part, which validEntry rejects.
		if freq > 0 && validEntry(e) {
			out = append(out, e)
		}
	}
	return out, nil
}

// SCELName returns the title stored in a standard .scel header, or "".
func SCELName(b []byte) string {
	if len(b) < 0x338 || string(b[:4]) != "\x40\x15\x00\x00" {
		return ""
	}
	s, err := utf16Text(b[0x130:0x338])
	if err != nil {
		return ""
	}
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
