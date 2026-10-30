// Package brand draws Shixin's original, font-independent 拾 mark. Packaging,
// the temporary tray anchor and the settings app share the same artwork.
package brand

import (
	"image"
	"image/color"

	"golang.org/x/image/vector"
)

// Icon draws the app icon on a transparent canvas, including the macOS inset.
// Render it at the destination size so small representations stay sharp.
func Icon(size int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	gradient := image.NewRGBA(dst.Bounds())
	for y := range size {
		for x := range size {
			t := float64(x+y) / float64(2*size)
			gradient.SetRGBA(x, y, color.RGBA{
				R: uint8(79 + 27*t), G: uint8(131 - 54*t), B: 255, A: 255,
			})
		}
	}
	r := vector.NewRasterizer(size, size)
	s := float32(size) / 1024
	m := func(x, y float32) { r.MoveTo(x*s, y*s) }
	l := func(x, y float32) { r.LineTo(x*s, y*s) }
	c := func(x1, y1, x2, y2, x3, y3 float32) {
		r.CubeTo(x1*s, y1*s, x2*s, y2*s, x3*s, y3*s)
	}
	// A rounded tile with the standard 100-point margin in a 1024 canvas.
	m(300, 100)
	l(724, 100)
	c(860, 100, 924, 164, 924, 300)
	l(924, 724)
	c(924, 860, 860, 924, 724, 924)
	l(300, 924)
	c(164, 924, 100, 860, 100, 724)
	l(100, 300)
	c(100, 164, 164, 100, 300, 100)
	r.ClosePath()
	r.Draw(dst, dst.Bounds(), gradient, image.Point{})
	mark(dst, color.White, 0.75, 0.125)
	return dst
}

// Template draws a black input-source icon with transparent surroundings.
// TISIconIsTemplate (and MyGo's IconIsTemplate for the temporary tray anchor)
// lets macOS tint it for light, dark and highlighted appearances.
func Template(size int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	mark(dst, color.Black, 1, 0)
	return dst
}

// mark draws geometric 扌 and 合 strokes on a 32-unit grid. The open counters
// and heavier strokes keep the character readable at 16 points. All paths
// have the same winding so overlapping strokes unite rather than cancel.
func mark(dst *image.RGBA, ink color.Color, scale, inset float32) {
	size := dst.Bounds().Dx()
	r := vector.NewRasterizer(size, size)
	unit := float32(size) * scale / 32
	offset := float32(size) * inset
	polygon := func(p ...float32) {
		r.MoveTo(offset+p[0]*unit, offset+p[1]*unit)
		for i := 2; i < len(p); i += 2 {
			r.LineTo(offset+p[i]*unit, offset+p[i+1]*unit)
		}
		r.ClosePath()
	}
	// 扌: cross stroke, hooked stem, rising stroke.
	polygon(3, 10, 13, 10, 13, 12.5, 3, 12.5)
	polygon(7.5, 5, 10, 5, 10, 25, 8, 27, 4.5, 27, 4.5, 24.5, 7.5, 24.5)
	polygon(3, 18, 13, 14, 13, 16.7, 3, 20.7)
	// 合: roof, cross stroke, and an open square counter.
	polygon(14, 12, 21, 5, 23, 5, 30, 12, 28.2, 14, 22, 8, 15.8, 14)
	polygon(17, 14.5, 27, 14.5, 27, 17, 17, 17)
	polygon(16, 19.5, 28, 19.5, 28, 27, 16, 27)
	// Reverse winding cuts out the mouth without erasing other strokes.
	polygon(18.5, 22, 18.5, 24.5, 25.5, 24.5, 25.5, 22)
	r.Draw(dst, dst.Bounds(), image.NewUniform(ink), image.Point{})
}
