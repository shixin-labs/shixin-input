package llama

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"

	"shixin/internal/dynlib"
)

// Load only our adjacent, explicitly named backends. ggml's automatic loader
// searches the working directory and environment; a typing helper must not.
// Keeping Vulkan in a separate module also lets machines without its driver
// load llama and report no accelerator, without ever running inference on CPU.
func (l *Lib) loadBackends(dir string) error {
	// The upstream override includes software CPU Vulkan devices and labels
	// them GPU. Only its default hardware-only enumeration is supported here.
	// Clear both Windows' environment and the shared CRT's getenv copy.
	if err := os.Unsetenv("GGML_VK_VISIBLE_DEVICES"); err != nil {
		return fmt.Errorf("llama: clear Vulkan device override: %w", err)
	}
	if err := windowsCRTEnvironment("GGML_VK_VISIBLE_DEVICES", ""); err != nil {
		return err
	}
	for _, name := range []string{"ggml.dll", "ggml-base.dll"} {
		h, err := dynlib.Open(filepath.Join(dir, name), false)
		if err != nil {
			return fmt.Errorf("llama: %s: %w", name, err)
		}
		l.dependencies = append(l.dependencies, h)
	}
	var register func(uintptr)
	var byName func(string) uintptr
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&register, "ggml_backend_register"},
		{&byName, "ggml_backend_reg_by_name"},
	} {
		sym, err := l.symbol(f.name)
		if err != nil {
			return fmt.Errorf("llama: %s: %w", f.name, err)
		}
		purego.RegisterFunc(f.fn, sym)
	}
	for _, backend := range []struct {
		file, name string
		optional   bool
	}{
		{"ggml-cpu.dll", "CPU", false},
		{"ggml-vulkan.dll", "Vulkan", true},
	} {
		if byName(backend.name) != 0 {
			continue
		}
		h, err := dynlib.Open(filepath.Join(dir, backend.file), false)
		if backend.optional && errors.Is(err, windows.ERROR_MOD_NOT_FOUND) {
			continue // No bundled backend or no system Vulkan loader/driver.
		}
		if err != nil {
			return fmt.Errorf("llama: %s: %w", backend.file, err)
		}
		sym, err := dynlib.Symbol(h, "ggml_backend_init")
		if err != nil {
			return fmt.Errorf("llama: %s: %w", backend.file, err)
		}
		var init func() uintptr
		purego.RegisterFunc(&init, sym)
		reg := init()
		if reg == 0 {
			if backend.optional {
				continue
			}
			return fmt.Errorf("llama: %s initialization failed", backend.file)
		}
		register(reg)
	}
	return nil
}

func windowsCRTEnvironment(name, value string) error {
	ucrt := windows.NewLazySystemDLL("ucrtbase.dll")
	sym := ucrt.NewProc("_putenv_s")
	if err := sym.Find(); err != nil {
		return fmt.Errorf("llama: shared CRT environment: %w", err)
	}
	var put func(string, string) int32
	purego.RegisterFunc(&put, sym.Addr())
	if code := put(name, value); code != 0 {
		return fmt.Errorf("llama: shared CRT environment: errno %d", code)
	}
	return nil
}
