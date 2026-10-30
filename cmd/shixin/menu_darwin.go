package main

import (
	"fmt"

	"github.com/ebitengine/purego/objc"

	"shixin/internal/client"
	"shixin/internal/i18n"
)

// The input menu: macOS shows the input controller's menu in the system
// input menu (the input source icon in the menu bar) while 拾新 is the
// current input source, so Shixin needs no menu bar item of its own. IMK
// sends a chosen item's action to the input controller (the selectors are
// registered in registerIMK). Built once on first use; main thread only.
var (
	inputMenu  objc.ID
	statusItem objc.ID // the first line: the input mode, or offline
	statusText = i18n.T("menu.mode.offline")
)

// actionSettings is the selector of the 设置… item.
const actionSettings = "openShixinSettings:"

// menuFor is IMKInputController's menu.
func menuFor(self objc.ID, cmd objc.SEL) objc.ID {
	if inputMenu == 0 {
		inputMenu = buildInputMenu()
	}
	return inputMenu
}

func buildInputMenu() objc.ID {
	m := objc.ID(objc.GetClass("NSMenu")).Send(objc.RegisterName("alloc")).
		Send(objc.RegisterName("initWithTitle:"), objc.ID(cstring(i18n.T("app.name")))) // kept for the process's life
	m.Send(objc.RegisterName("setAutoenablesItems:"), false)
	add := func(title, action string) objc.ID {
		var sel objc.SEL
		if action != "" {
			sel = objc.RegisterName(action)
		}
		it := m.Send(objc.RegisterName("addItemWithTitle:action:keyEquivalent:"), objc.ID(cstring(title)), sel, objc.ID(cstring("")))
		it.Send(objc.RegisterName("setEnabled:"), action != "")
		return it
	}
	separator := func() {
		m.Send(objc.RegisterName("addItem:"), objc.ID(objc.GetClass("NSMenuItem")).Send(objc.RegisterName("separatorItem")))
	}
	statusItem = add(statusText, "")
	separator()
	add(i18n.T("menu.settings"), actionSettings)
	separator()
	add(fmt.Sprintf(i18n.T("menu.version"), version), "")
	return m
}

func menuOpenSettings(self objc.ID, cmd objc.SEL, sender objc.ID) { openSettings() }

// setStatus shows the input mode on the input menu's first line.
func setStatus(s client.Status) {
	switch {
	case !s.Connected:
		statusText = i18n.T("menu.mode.offline")
	case s.ASCII:
		statusText = i18n.T("menu.mode.english")
	default:
		statusText = i18n.T("menu.mode.chinese")
	}
	if statusItem != 0 {
		statusItem.Send(objc.RegisterName("setTitle:"), objc.ID(cstring(statusText)))
	}
}
