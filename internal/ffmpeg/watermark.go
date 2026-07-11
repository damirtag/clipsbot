package ffmpeg

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// generateWatermarkPNG renders text to a transparent PNG with a black
// outline behind semi-transparent white fill, then writes it to outPath.
//
// Rendering the watermark ourselves in pure Go means the ffmpeg binary
// only ever needs the `overlay` filter, which is present in essentially
// every ffmpeg build, so this doesn't silently break again on a different
// machine or in a container.
//
// The rendering uses golang.org/x/image's basicfont (a small bitmap font
// shipped with the package — no system fonts, no cgo, no fontconfig), then
// scales the result up for legibility. It's blockier than a TrueType
// render would be; if you want smoother text later, swap this for
// golang.org/x/image/font/opentype with an embedded .ttf and the rest of
// the pipeline (overlay-based compositing) doesn't need to change at all.
func generateWatermarkPNG(text string, outPath string) error {
	const (
		scale     = 3 // integer upscale of the bitmap font for legibility
		padding   = 8 // px padding around text, pre-scale
		fillAlpha = 70
		outlineA  = 40
	)

	face := basicfont.Face7x13
	textWidth := font.MeasureString(face, text).Ceil()
	ascent := face.Metrics().Ascent.Ceil()
	descent := face.Metrics().Descent.Ceil()
	lineHeight := ascent + descent

	baseW := textWidth + padding*2
	baseH := lineHeight + padding*2

	base := image.NewRGBA(image.Rect(0, 0, baseW, baseH))
	// fully transparent background
	draw.Draw(base, base.Bounds(), &image.Uniform{C: color.RGBA{0, 0, 0, 0}}, image.Point{}, draw.Src)

	origin := fixed.Point26_6{
		X: fixed.I(padding),
		Y: fixed.I(padding + ascent),
	}

	black := &image.Uniform{C: color.RGBA{0, 0, 0, outlineA}}
	white := &image.Uniform{C: color.RGBA{180, 180, 180, fillAlpha}}

	// Outline: draw the text 8 times, offset by 1px in each direction, in
	// black, before drawing the white fill on top. Cheap and looks fine at
	// this scale once upscaled.
	offsets := []struct{ dx, dy int }{
		{-1, 0},
		{1, 0},
		{0, -1},
		{0, 1},
	}
	for _, o := range offsets {
		drawer := &font.Drawer{
			Dst:  base,
			Src:  black,
			Face: face,
			Dot: fixed.Point26_6{
				X: origin.X + fixed.I(o.dx),
				Y: origin.Y + fixed.I(o.dy),
			},
		}
		drawer.DrawString(text)
	}

	fillDrawer := &font.Drawer{
		Dst:  base,
		Src:  white,
		Face: face,
		Dot:  origin,
	}
	fillDrawer.DrawString(text)

	// Nearest-neighbor upscale for a bigger, more visible watermark than
	// the native 7x13 bitmap font would give at 1x.
	scaled := image.NewRGBA(image.Rect(0, 0, baseW*scale, baseH*scale))
	for y := 0; y < scaled.Bounds().Dy(); y++ {
		for x := 0; x < scaled.Bounds().Dx(); x++ {
			scaled.Set(x, y, base.At(x/scale, y/scale))
		}
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, scaled)
}
