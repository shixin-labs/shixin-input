package i18n

import "testing"

func TestCatalogComplete(t *testing.T) {
	for k, v := range catalog {
		if v[0] == "" || v[1] == "" {
			t.Errorf("%s lacks a translation", k)
		}
	}
	SetLang(English)
	if T("menu.settings") != "Settings…" {
		t.Error(T("menu.settings"))
	}
	SetLang(Chinese)
	if T("menu.settings") != "设置…" || T("no.such.key") != "no.such.key" {
		t.Error("lookup")
	}
}
