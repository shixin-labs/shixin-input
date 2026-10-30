package settings

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPunctuation(t *testing.T) {
	for _, style := range []string{"chinese", "english"} {
		t.Run(style, func(t *testing.T) {
			s := Default()
			s.Punctuation = style
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := Save(path, s); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil || !reflect.DeepEqual(got, s) {
				t.Fatalf("round trip: %+v, %v", got, err)
			}
		})
	}
	for _, tc := range []struct {
		json string
		bad  bool
	}{
		{`{"page_size":7}`, false},
		{`{"page_size":7,"punctuation":"invalid"}`, true},
	} {
		got, err := Parse([]byte(tc.json))
		if (err != nil) != tc.bad || got.Punctuation != "chinese" || got.PageSize != 7 {
			t.Errorf("parse %s: %+v, %v", tc.json, got, err)
		}
	}
}
