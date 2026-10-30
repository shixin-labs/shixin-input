// Package generation builds private, immutable Rime generations. The
// builder receives no live user directory and never creates a session.
package generation

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"shixin/internal/rime"
	"shixin/internal/settings"
)

// MatchesSettings reports whether a generation's saved compiler output
// matches this binary's compiler. Loading its settings snapshot alone is
// insufficient: newly added defaults can make old and new snapshots look
// identical even though the deployed key bindings differ.
// Call only outside the engine thread, before reusing a saved generation.
func MatchesSettings(dir string, s settings.Settings) (bool, error) {
	files, err := settings.Compile(s)
	if err != nil {
		return false, err
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, "input", name))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("generation: read saved compiler output %s: %w", name, err)
		}
		if !bytes.Equal(got, want) {
			return false, nil
		}
	}
	return true, nil
}

// Options configures an offline build into a new, empty directory.
type Options struct {
	Lib, Data, Prebuilt, Dir string
	Settings                 settings.Settings
	All                      bool // compile every supported schema for packaging
}

// Build compiles config and dictionaries using only deployer APIs. Run it
// in a separate process: librime's service is process-global.
func Build(o Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return err
	}
	// librime takes an up-to-date compiled config from the prebuilt dir
	// instead of writing it to staging, where Build edits it.
	if yamls, _ := filepath.Glob(filepath.Join(o.Prebuilt, "*.yaml")); o.Prebuilt != "" && len(yamls) > 0 {
		return errors.New("generation: prebuilt data must hold only compiled dictionaries")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("generation: new directory: %w", err)
	}
	input := filepath.Join(dir, "input")
	if err := os.Mkdir(input, 0o700); err != nil {
		return err
	}
	if _, err := settings.Apply(input, o.Settings); err != nil {
		return err
	}
	if err := stableInputs(o.Data, input); err != nil {
		return err
	}
	if err := settings.Save(filepath.Join(dir, "settings.json"), o.Settings); err != nil {
		return err
	}
	lib, err := rime.Load(o.Lib)
	if err != nil {
		return err
	}
	lib.Setup(rime.Traits{SharedDataDir: o.Data, UserDataDir: input, StagingDir: filepath.Join(dir, "build"), PrebuiltDataDir: o.Prebuilt,
		LogDir: "", MinLogLevel: 2, AppName: "rime.shixin.builder", Modules: []string{"deployer"}})
	lib.InitializeDeployer()
	defer lib.Finalize()
	if !lib.DeployConfig("default.yaml", "config_version") {
		return errors.New("generation: deploy default failed")
	}
	ids := []string{o.Settings.Scheme}
	if o.All {
		ids = nil
		for _, s := range settings.Schemes {
			ids = append(ids, s.ID)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if err := deploySchema(lib, dir, input, id, seen); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if err := addWarmSchema(dir, input, id); err != nil {
			return err
		}
	}
	if err := listWarmSchemas(dir, ids); err != nil {
		return err
	}
	return Validate(dir)
}

// deploySchema compiles schema id and the schemas it depends on, once
// each, and rejects any with a Lua component.
func deploySchema(lib *rime.Lib, dir, input, id string, seen map[string]bool) error {
	if filepath.Base(id) != id {
		return errors.New("generation: invalid schema dependency")
	}
	if seen[id] {
		return nil
	}
	seen[id] = true
	if !lib.DeploySchema(filepath.Join(input, id+".schema.yaml")) {
		return fmt.Errorf("generation: deploy schema %s failed", id)
	}
	m, err := readMap(filepath.Join(dir, "build", id+".schema.yaml"))
	if err != nil {
		return err
	}
	if err := checkComponents(m, false); err != nil {
		return fmt.Errorf("generation: %s: %w", id, err)
	}
	schema, _ := m["schema"].(map[string]any)
	deps, _ := schema["dependencies"].([]any)
	for _, v := range deps {
		dep, ok := v.(string)
		if !ok {
			return errors.New("generation: invalid schema dependency")
		}
		if err := deploySchema(lib, dir, input, dep, seen); err != nil {
			return err
		}
	}
	return nil
}

// addWarmSchema points compiled schema id at the generation's own phrase
// table and writes its warm variant, <id>_warm, with every translator's
// user dictionary disabled.
func addWarmSchema(dir, input, id string) error {
	path := filepath.Join(dir, "build", id+".schema.yaml")
	m, err := readMap(path)
	if err != nil {
		return err
	}
	// stabledb accepts absolute resource paths. Each generation owns its
	// phrase table, so building a replacement cannot alter the live one.
	if ns, ok := m["custom_phrase"].(map[string]any); ok {
		ns["user_dict"] = filepath.Join(input, settings.PhraseTable)
	}
	if err := writeMap(path, m); err != nil {
		return err
	}
	m["schema"].(map[string]any)["schema_id"] = id + "_warm"
	for _, ns := range translatorNamespaces(m) {
		cfg, ok := m[ns].(map[string]any)
		if !ok {
			cfg = map[string]any{}
			m[ns] = cfg
		}
		cfg["enable_user_dict"] = false
	}
	return writeMap(filepath.Join(dir, "build", id+"_warm.schema.yaml"), m)
}

// listWarmSchemas makes the warm variants of ids the only schemas in the
// compiled default.yaml.
func listWarmSchemas(dir string, ids []string) error {
	path := filepath.Join(dir, "build", "default.yaml")
	d, err := readMap(path)
	if err != nil {
		return err
	}
	var list []any
	for _, id := range ids {
		list = append(list, map[string]any{"schema": id + "_warm"})
	}
	d["schema_list"] = list
	return writeMap(path, d)
}

// inputTime is the modification time of every generation input.
var inputTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// stableInputs copies the shared configuration YAML into the generation's
// input dir, next to the files settings generated, and gives all of them
// inputTime. librime records its sources' mtimes in each compiled schema
// and rebuilds a prism whenever its schema's bytes change, so without
// this every generation rebuilds every prism (2 s for rime-ice), and the
// bundle's prebuilt prisms are never used. Dictionary sources stay in the
// shared dir: the bundle ships their compiled tables instead.
func stableInputs(data, input string) error {
	files, err := filepath.Glob(filepath.Join(data, "*.yaml"))
	if err != nil {
		return err
	}
	for _, src := range files {
		dst := filepath.Join(input, filepath.Base(src))
		if strings.HasSuffix(src, ".dict.yaml") {
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			continue // generated from the settings
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(input)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.Chtimes(filepath.Join(input, e.Name()), inputTime, inputTime); err != nil {
			return err
		}
	}
	return nil
}

// Validate rejects a generation unless its default list contains only
// warm schemas and every script/table translator disables user learning.
// It runs before librime creates any standby session.
func Validate(dir string) error {
	d, err := readMap(filepath.Join(dir, "build", "default.yaml"))
	if err != nil {
		return err
	}
	list, ok := d["schema_list"].([]any)
	if !ok || len(list) == 0 {
		return errors.New("generation: empty warm schema list")
	}
	for _, v := range list {
		item, _ := v.(map[string]any)
		id, _ := item["schema"].(string)
		if !strings.HasSuffix(id, "_warm") || filepath.Base(id) != id {
			return errors.New("generation: unsafe default schema")
		}
		m, err := readMap(filepath.Join(dir, "build", id+".schema.yaml"))
		if err != nil {
			return err
		}
		schema, _ := m["schema"].(map[string]any)
		if schema["schema_id"] != id {
			return errors.New("generation: mismatched warm schema")
		}
		if err := checkComponents(m, true); err != nil {
			return err
		}
	}
	return nil
}

// userDictTranslators are the translators that can open a user dictionary.
var userDictTranslators = map[string]bool{
	"script_translator": true, "table_translator": true, "reverse_lookup_translator": true,
}

// warmTranslators are the translators a warm schema may list: the user
// dictionary ones, which it configures off, and those without one.
var warmTranslators = map[string]bool{
	"script_translator": true, "table_translator": true, "reverse_lookup_translator": true,
	"punct_translator": true, "echo_translator": true,
	"shixin_date_translator": true, "shixin_unicode_translator": true,
	"shixin_number_translator": true, "shixin_calc_translator": true,
}

// translatorNamespaces returns the config namespaces of the schema's
// translators that can open a user dictionary.
func translatorNamespaces(m map[string]any) []string {
	engine, _ := m["engine"].(map[string]any)
	list, _ := engine["translators"].([]any)
	var out []string
	for _, v := range list {
		name, _ := v.(string)
		base, ns, _ := strings.Cut(name, "@")
		if !userDictTranslators[base] {
			continue
		}
		if ns == "" {
			ns = "translator"
		}
		out = append(out, ns)
	}
	return out
}

// checkComponents rejects Lua components, and in a warm schema, any
// translator that isn't known to be safe or can open a user dictionary.
func checkComponents(m map[string]any, warm bool) error {
	engine, ok := m["engine"].(map[string]any)
	if !ok {
		return errors.New("generation: missing engine")
	}
	for _, key := range []string{"processors", "segmentors", "translators", "filters"} {
		list, _ := engine[key].([]any)
		for _, v := range list {
			name, _ := v.(string)
			if strings.Contains(name, "lua_") {
				return errors.New("generation: Lua component forbidden")
			}
			if warm && key == "translators" {
				if base, _, _ := strings.Cut(name, "@"); !warmTranslators[base] {
					return errors.New("generation: unsupported warm translator")
				}
			}
		}
	}
	if warm {
		for _, ns := range translatorNamespaces(m) {
			cfg, _ := m[ns].(map[string]any)
			if off, ok := cfg["enable_user_dict"].(bool); !ok || off {
				return fmt.Errorf("generation: warm translator %s can open a user dictionary", ns)
			}
		}
	}
	return nil
}

func readMap(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("generation: invalid compiled YAML in %s", filepath.Base(path))
	}
	return m, nil
}

func writeMap(path string, m map[string]any) error {
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
