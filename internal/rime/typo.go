package rime

import (
	"errors"
	"fmt"
	"shixin/internal/dynlib"
)

// BuildTypoResources is offline builder work, never an engine key-path call.
// It opens a compiled system table and creates two new generation-local files;
// no session or user dictionary is opened.
func (l *Lib) BuildTypoResources(table, initial, mirror string) error {
	fn, err := dynlib.Symbol(l.handle, "shixin_typo_build_resources_v1")
	if err != nil {
		return errors.New("rime: typo correction requires a matching native library")
	}
	result := int32(l.call(fn, l.cstr(table), l.cstr(initial), l.cstr(mirror)))
	if result != 0 {
		return fmt.Errorf("rime: build typo resources failed (%d)", result)
	}
	return nil
}
