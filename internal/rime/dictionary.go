package rime

import (
	"errors"
	"runtime"
	"unsafe"
)

// cModule and cLevers mirror rime_api.h and rime_levers_api.h. The public
// levers table predates librime 1.10; validate its size before calling it.
type cModule struct {
	dataSize                           int32
	name, initialize, finalize, getAPI uintptr
}
type cLevers struct {
	dataSize  int32
	functions [32]uintptr
}

// DictionaryTable transfers portable full-pinyin tables on the maintenance
// child's locked OS thread. It must never be called by a serving engine.
// The child uses a private database copy and creates no sessions.
func (l *Lib) DictionaryTable(path string, importTable bool) (int, error) {
	return l.transfer("rime_ice", path, importTable)
}

// ExportUserDictionary writes the named user database in the deployer's
// user directory as a Rime table. The engine uses it on a private copy of
// another Rime input method's database, in a child that owns no Shixin
// user dictionary and creates no sessions.
func (l *Lib) ExportUserDictionary(dict, path string) (int, error) {
	return l.transfer(dict, path, false)
}

func (l *Lib) transfer(dictName, path string, importTable bool) (int, error) {
	name := append([]byte("levers"), 0)
	p := l.call(l.api.findModule, uintptr(unsafe.Pointer(&name[0])))
	runtime.KeepAlive(name)
	if p == 0 {
		return 0, errors.New("rime: dictionary management is unavailable")
	}
	m := (*cModule)(cptr(p))
	if uintptr(m.dataSize)+4 < unsafe.Sizeof(cModule{}) || m.getAPI == 0 {
		return 0, errors.New("rime: unsupported levers module")
	}
	p = l.call(m.getAPI)
	if p == 0 {
		return 0, errors.New("rime: missing levers API")
	}
	api := (*cLevers)(cptr(p))
	i := 29 // export_user_dict; import_user_dict is the following member.
	if importTable {
		i++
	}
	if uintptr(api.dataSize)+4 < unsafe.Offsetof(api.functions)+uintptr(i+1)*unsafe.Sizeof(uintptr(0)) || api.functions[i] == 0 {
		return 0, errors.New("rime: unsupported dictionary API")
	}
	dict, file := append([]byte(dictName), 0), append([]byte(path), 0)
	n := int(int32(l.call(api.functions[i], uintptr(unsafe.Pointer(&dict[0])), uintptr(unsafe.Pointer(&file[0])))))
	runtime.KeepAlive(dict)
	runtime.KeepAlive(file)
	if n < 0 {
		return 0, errors.New("rime: dictionary transfer failed")
	}
	return n, nil
}
