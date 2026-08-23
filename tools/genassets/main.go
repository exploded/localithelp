// Command genassets renders the site's icon set and Open Graph image into
// static/img. It's a dev-time tool — the PNGs are committed, and this only needs
// re-running if the palette or wording changes:
//
//	go run ./tools/genassets
//
// It uses the Go fonts bundled with golang.org/x/image so there's nothing to download.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Palette — mirrors static/css/styles.css.
var (
	bgDeep   = color.RGBA{0xf4, 0xf6, 0xfa, 0xff}
	surface  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	brand    = color.RGBA{0x1d, 0x4e, 0xd8, 0xff}
	accent   = color.RGBA{0xb4, 0x53, 0x09, 0xff}
	textPri  = color.RGBA{0x0f, 0x17, 0x2a, 0xff}
	textSec  = color.RGBA{0x47, 0x55, 0x69, 0xff}
	textMute = color.RGBA{0x5b, 0x6b, 0x82, 0xff}
	white    = color.RGBA{0xff, 0xff, 0xff, 0xff}
)

// Dark variants, used by coverImage. The light palette above is tuned for a
// white page; on a navy field the brand blue and amber both drop below a legible
// contrast ratio, so the cover uses brighter siblings of the same hues.
var (
	navyTop  = color.RGBA{0x0b, 0x12, 0x20, 0xff}
	navyBot  = color.RGBA{0x17, 0x2a, 0x52, 0xff}
	brandLt  = color.RGBA{0x3b, 0x82, 0xf6, 0xff}
	accentLt = color.RGBA{0xf5, 0x9e, 0x0b, 0xff}
	dimOnDk  = color.RGBA{0xc3, 0xd0, 0xe3, 0xff}
	faintOn  = color.RGBA{0x8d, 0x9d, 0xb5, 0xff}
)

func main() {
	out := "static/img"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	must(writePNG(filepath.Join(out, "og.png"), ogImage()))
	must(writePNG(filepath.Join(out, "square.png"), squareImage()))
	must(writePNG(filepath.Join(out, "cover.png"), coverImage()))
	for _, sz := range []int{32, 180, 192, 512, 1024} {
		name := fmt.Sprintf("icon-%d.png", sz)
		if sz == 180 {
			name = "apple-touch-icon.png"
		}
		must(writePNG(filepath.Join(out, name), icon(sz)))
	}
	must(writeICO(filepath.Join(out, "favicon.ico"), icon(16), icon(32), icon(48)))
	if out == "static/img" {
		// The invoice/receipt PDF embeds its own copy of the icon.
		must(writePNG("cmd/server/logo.png", icon(512)))
	}
	fmt.Println("wrote assets to", out)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(f, img)
}

// writeICO wraps PNG-encoded images in an ICO container (PNG-in-ICO is
// supported by every browser since Vista-era Windows).
func writeICO(path string, imgs ...image.Image) error {
	var pngs [][]byte
	for _, im := range imgs {
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			return err
		}
		pngs = append(pngs, b.Bytes())
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&out, binary.LittleEndian, uint16(len(imgs)))
	offset := 6 + 16*len(imgs)
	for i, im := range imgs {
		w, h := im.Bounds().Dx(), im.Bounds().Dy()
		if w >= 256 {
			w = 0
		}
		if h >= 256 {
			h = 0
		}
		out.WriteByte(byte(w))
		out.WriteByte(byte(h))
		out.WriteByte(0)                                    // colour count
		out.WriteByte(0)                                    // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&out, binary.LittleEndian, uint16(32)) // bpp
		binary.Write(&out, binary.LittleEndian, uint32(len(pngs[i])))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// ── drawing helpers ──

func face(ttf []byte, size float64) font.Face {
	f, err := opentype.Parse(ttf)
	if err != nil {
		log.Fatal(err)
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		log.Fatal(err)
	}
	return fc
}

func text(dst draw.Image, fc font.Face, s string, x, y int, col color.Color) int {
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(col), Face: fc, Dot: fixed.P(x, y)}
	d.DrawString(s)
	return d.Dot.X.Ceil()
}

func textWidth(fc font.Face, s string) int {
	d := &font.Drawer{Face: fc}
	return d.MeasureString(s).Ceil()
}

func fill(dst draw.Image, r image.Rectangle, col color.Color) {
	draw.Draw(dst, r, image.NewUniform(col), image.Point{}, draw.Src)
}

// roundedRect fills a rounded rectangle with anti-aliased corners.
func roundedRect(dst *image.RGBA, r image.Rectangle, radius float64, col color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			a := coverage(float64(x)+0.5, float64(y)+0.5, r, radius)
			if a <= 0 {
				continue
			}
			c := col
			if a < 1 {
				c = blend(dst.RGBAAt(x, y), col, a)
			}
			dst.SetRGBA(x, y, c)
		}
	}
}

// coverage returns 0..1 for how much of the pixel at (px,py) is inside the
// rounded rect (approximate: distance-based, gives ~1px AA on the arcs).
func coverage(px, py float64, r image.Rectangle, radius float64) float64 {
	x0, y0 := float64(r.Min.X)+radius, float64(r.Min.Y)+radius
	x1, y1 := float64(r.Max.X)-radius, float64(r.Max.Y)-radius
	cx := clamp(px, x0, x1)
	cy := clamp(py, y0, y1)
	dx, dy := px-cx, py-cy
	d := sqrt(dx*dx+dy*dy) - radius
	switch {
	case d <= -0.5:
		return 1
	case d >= 0.5:
		return 0
	default:
		return 0.5 - d
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sqrt(v float64) float64 {
	// Newton's method is plenty here and avoids importing math for one call.
	if v <= 0 {
		return 0
	}
	x := v
	for i := 0; i < 20; i++ {
		x = 0.5 * (x + v/x)
	}
	return x
}

// blend composites an opaque fg over bg with coverage a (0..1). image.RGBA is
// premultiplied, so the "over" operator is a straight lerp per channel with the
// alpha channel included — this keeps AA edges clean on transparent backgrounds.
func blend(bg, fg color.RGBA, a float64) color.RGBA {
	mix := func(b, f uint8) uint8 { return uint8(float64(b)*(1-a) + float64(f)*a + 0.5) }
	return color.RGBA{mix(bg.R, fg.R), mix(bg.G, fg.G), mix(bg.B, fg.B), mix(bg.A, 0xff)}
}

// softGlow paints a faint radial blob — echoes the site's glow-orb backgrounds.
func softGlow(dst *image.RGBA, cx, cy, radius float64, col color.RGBA, maxAlpha float64) {
	b := dst.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := sqrt(dx*dx+dy*dy) / radius
			if d >= 1 {
				continue
			}
			a := (1 - d) * (1 - d) * maxAlpha
			dst.SetRGBA(x, y, blend(dst.RGBAAt(x, y), col, a))
		}
	}
}

// ── the images ──

func icon(size int) *image.RGBA { return iconTile(size, brand, accent) }

// iconTile draws the "IT." mark at the given size. tile is the rounded square's
// colour and dot the full stop's; icon() passes the on-white palette, the cover
// passes the brighter on-navy one.
func iconTile(size int, tile, dot color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Transparent background; a rounded tile with "IT".
	roundedRect(img, img.Bounds(), float64(size)*0.22, tile)
	fc := face(gobold.TTF, float64(size)*0.5)
	w := textWidth(fc, "IT")
	m := fc.Metrics()
	// Vertically centre using cap height approximation (ascent*0.72).
	capH := m.Ascent.Ceil() * 72 / 100
	x := (size - w) / 2
	y := (size+capH)/2 - size/40
	text(img, fc, "IT", x, y, white)
	// Small amber dot after the T — the wordmark's full stop — only when there's room.
	if size >= 48 {
		r := float64(size) * 0.055
		cx, cy := float64(x+w)+r*1.6, float64(y)-r
		roundedRect(img, image.Rect(int(cx-r), int(cy-r), int(cx+r)+1, int(cy+r)+1), r, dot)
	}
	return img
}

// textC draws s centred on cx and returns the x it ended at.
func textC(dst draw.Image, fc font.Face, s string, cx, y int, col color.Color) int {
	return text(dst, fc, s, cx-textWidth(fc, s)/2, y, col)
}

// brandPlate draws the centred brand lockup — mark, wordmark, rule, strapline,
// locality, promise chips and domain — full-bleed on a dark gradient field.
// Cover and photo differ only in canvas shape and scale, so they share it.
//
// safeW and safeH are the narrowest and shortest crops the slot can take, less
// a margin: everything is held inside them so no crop can clip a word. Google
// Business Profile re-crops per surface, so nothing is drawn near an edge
// either — a crop that sliced through a card border would look broken.
func brandPlate(img *image.RGBA, scale float64, safeW, safeH int) {
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	cx := W / 2
	s := func(v int) int { return int(float64(v)*scale + 0.5) }

	gradient(img, navyTop, navyBot)
	softGlow(img, float64(W)*0.90, float64(H)*0.09, float64(W)*0.47, brandLt, 0.30)
	softGlow(img, float64(W)*0.10, float64(H)*0.98, float64(W)*0.40, accentLt, 0.10)

	kicker := face(gomono.TTF, 19*scale)
	title := face(gobold.TTF, 66*scale)
	sub := face(goregular.TTF, 30*scale)
	small := face(goregular.TTF, 21*scale)

	// Offsets down from the top of the mark, at scale 1. The block is measured
	// and centred as a whole, so it sits right on any canvas shape.
	const (
		markH   = 132
		wordY   = 237 // baselines
		ruleY   = 266
		subY    = 324
		kickY   = 364
		chipY   = 414 // top of the chip row
		chipH   = 42
		domainY = 502
		stackH  = 512
	)
	mustFit(s(stackH), safeH, "stack height")
	top := (H - s(stackH)) / 2
	at := func(off int) int { return top + s(off) }

	// The mark, big and centred — these slots have room for it, the favicon doesn't.
	mark := s(markH)
	softGlow(img, float64(cx), float64(top+mark/2), float64(mark)*1.9, brandLt, 0.22)
	draw.Draw(img, image.Rect(cx-mark/2, top, cx+mark/2, top+mark),
		iconTile(mark, brandLt, accentLt), image.Point{}, draw.Over)

	// Wordmark plus its amber full stop, centred as one unit.
	word, stop := "LOCAL IT HELP", "."
	w := textWidth(title, word) + textWidth(title, stop)
	end := text(img, title, word, cx-w/2, at(wordY), white)
	text(img, title, stop, end, at(wordY), accentLt)
	mustFit(w, safeW, "wordmark")

	// A short amber rule ties the wordmark to the strapline.
	fill(img, image.Rect(cx-s(45), at(ruleY), cx+s(45), at(ruleY)+s(4)), accentLt)

	textC(img, sub, "Friendly computer help, at your place.", cx, at(subY), dimOnDk)
	textC(img, kicker, "DONVALE  ·  MELBOURNE'S EAST", cx, at(kickY), faintOn)

	// Promise chips, centred as a row: outlined rather than filled, so they read
	// as trim on the dark field instead of competing with the wordmark.
	chips := []string{"No fix, no fee", "Same or next day", "14-day guarantee"}
	gap, padX := s(13), s(16)
	total := gap * (len(chips) - 1)
	for _, c := range chips {
		total += textWidth(small, c) + padX*2
	}
	mustFit(total, safeW, "chip row")
	x := cx - total/2
	for _, c := range chips {
		bw := textWidth(small, c) + padX*2
		box := image.Rect(x, at(chipY), x+bw, at(chipY)+s(chipH))
		chipOutline(img, box, float64(s(21)))
		text(img, small, c, x+padX, box.Max.Y-s(13), dimOnDk)
		x += bw + gap
	}

	textC(img, kicker, "localithelp.com.au", cx, at(domainY), white)
}

// coverImage is the 16:9 cover, sized for Google Business Profile. Unlike
// ogImage — a link-preview card that is only ever shown whole — this is
// full-bleed, because the worst case here is a centred square crop, which of a
// 1200×675 image keeps only the middle 675px.
func coverImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 675))
	brandPlate(img, 1, 620, 675) // a square crop keeps the full height, so only width binds
	return img
}

// squareImage is the 1:1 variant, for the Google Business Profile photos slot.
// Photos are shown square or cropped to 4:3 landscape — which of a square keeps
// the middle 900px of height and none of the width — so the height is the tight
// dimension here, the reverse of the cover. It runs the lockup larger, because
// photos are usually seen small, as a thumbnail beside the profile.
func squareImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 1200))
	brandPlate(img, 1.55, 1000, 880)
	return img
}

// mustFit fails the build rather than shipping a cover a crop would clip — the
// wording here changes more often than anyone re-checks the crop.
func mustFit(w, safe int, what string) {
	if w > safe {
		log.Fatalf("%s is %dpx wide, over the %dpx crop budget", what, w, safe)
	}
}

// chipOutline strokes a rounded rectangle by filling one and knocking out an
// inset copy, which keeps the anti-aliasing consistent with roundedRect.
func chipOutline(dst *image.RGBA, r image.Rectangle, radius float64) {
	stroke := color.RGBA{0x3f, 0x52, 0x74, 0xff}
	before := image.NewRGBA(r)
	draw.Draw(before, r, dst, r.Min, draw.Src)
	roundedRect(dst, r, radius, stroke)
	in := r.Inset(2)
	for y := in.Min.Y; y < in.Max.Y; y++ {
		for x := in.Min.X; x < in.Max.X; x++ {
			a := coverage(float64(x)+0.5, float64(y)+0.5, in, radius-2)
			if a <= 0 {
				continue
			}
			dst.SetRGBA(x, y, blend(dst.RGBAAt(x, y), before.RGBAAt(x, y), a))
		}
	}
}

// gradient paints a diagonal two-stop fade across the whole image.
func gradient(dst *image.RGBA, from, to color.RGBA) {
	b := dst.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	lerp := func(a, c uint8, t float64) uint8 { return uint8(float64(a)*(1-t) + float64(c)*t + 0.5) }
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			t := clamp(float64(x)/w*0.35+float64(y)/h*0.65, 0, 1)
			dst.SetRGBA(x, y, color.RGBA{
				lerp(from.R, to.R, t), lerp(from.G, to.G, t), lerp(from.B, to.B, t), 0xff,
			})
		}
	}
}

func ogImage() *image.RGBA {
	const W, H = 1200, 630
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fill(img, img.Bounds(), bgDeep)
	softGlow(img, 1050, 90, 420, accent, 0.18)
	softGlow(img, 120, 600, 380, brand, 0.12)

	// Card
	card := image.Rect(80, 80, W-80, H-80)
	roundedRect(img, card, 22, surface)
	// Left brand rule
	fill(img, image.Rect(card.Min.X, card.Min.Y+40, card.Min.X+6, card.Max.Y-40), brand)

	kicker := face(gomono.TTF, 22)
	title := face(gobold.TTF, 84)
	sub := face(goregular.TTF, 32)
	small := face(goregular.TTF, 24)

	x := card.Min.X + 64
	text(img, kicker, "COMPUTER HELP  ·  DONVALE & MELBOURNE'S EAST", x, card.Min.Y+84, textMute)

	// Wordmark with brand-coloured full stop
	end := text(img, title, "LOCAL IT HELP", x, card.Min.Y+200, textPri)
	text(img, title, ".", end, card.Min.Y+200, brand)

	text(img, sub, "Friendly computer help, at your place.", x, card.Min.Y+262, textSec)
	text(img, sub, "Email · printers · Wi-Fi · scams · slow PCs · new setups", x, card.Min.Y+306, textSec)
	text(img, sub, "— plus Shopify, websites & custom software.", x, card.Min.Y+350, textSec)

	// Footer strip: promise chips
	y := card.Max.Y - 62
	chips := []string{"No fix, no fee", "Same or next day", "14-day guarantee"}
	cx := x
	for _, c := range chips {
		w := textWidth(small, c)
		box := image.Rect(cx, y-30, cx+w+36, y+12)
		roundedRect(img, box, 21, color.RGBA{0xee, 0xf2, 0xf7, 0xff})
		text(img, small, c, cx+18, y, textPri)
		cx += w + 36 + 14
	}
	text(img, kicker, "localithelp.com.au", card.Max.X-64-textWidth(kicker, "localithelp.com.au"), y, brand)
	return img
}
