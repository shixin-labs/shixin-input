// Command icons renders the shared brand artwork for macOS packaging. Only
// the Go sources are checked in; bundle-macos.sh builds the TIFF/ICNS resources.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"shixin/internal/ui/brand"
)

func main() {
	out := flag.String("output", "build/macos/icons", "directory for PNG representations")
	flag.Parse()
	if err := render(*out); err != nil {
		log.Fatal(err)
	}
}

func render(out string) error {
	set := filepath.Join(out, "Shixin.iconset")
	if err := os.MkdirAll(set, 0o755); err != nil {
		return fmt.Errorf("create iconset: %w", err)
	}
	write := func(path string, img image.Image) error {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			return fmt.Errorf("encode %s: %w", path, err)
		}
		return f.Close()
	}
	for _, points := range []int{16, 32, 128, 256, 512} {
		for _, scale := range []int{1, 2} {
			suffix := ""
			if scale == 2 {
				suffix = "@2x"
			}
			name := fmt.Sprintf("icon_%dx%d%s.png", points, points, suffix)
			if err := write(filepath.Join(set, name), brand.Icon(points*scale)); err != nil {
				return err
			}
		}
	}
	for _, size := range []int{16, 32} {
		if err := write(filepath.Join(out, fmt.Sprintf("InputSource-%d.png", size)), brand.Template(size)); err != nil {
			return err
		}
	}
	return nil
}
