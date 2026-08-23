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

func icon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Transparent background; a brand-blue rounded tile with "IT".
	roundedRect(img, img.Bounds(), float64(size)*0.22, brand)
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
		roundedRect(img, image.Rect(int(cx-r), int(cy-r), int(cx+r)+1, int(cy+r)+1), r, accent)
	}
	return img
}

// textC draws s centred on cx and returns the x it ended at.
func textC(dst draw.Image, fc font.Face, s string, cx, y int, col color.Color) int {
	return text(dst, fc, s, cx-textWidth(fc, s)/2, y, col)
}

// coverImage is the 16:9 cover, sized for Google Business Profile. GBP re-crops
// the cover to whatever shape the surface wants, and a square crop of a 16:9
// image keeps only the middle 675px — so everything here is centred and held
// inside that column rather than run across the full width like ogImage.
func coverImage() *image.RGBA {
	const W, H = 1200, 675
	const safe = 600 // widest any text may be: survives a centred square crop
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fill(img, img.Bounds(), bgDeep)
	softGlow(img, 1010, 110, 430, accent, 0.18)
	softGlow(img, 150, 600, 400, brand, 0.12)

	card := image.Rect(70, 60, W-70, H-60)
	roundedRect(img, card, 22, surface)

	cx := W / 2
	kicker := face(gomono.TTF, 18)
	title := face(gobold.TTF, 60)
	sub := face(goregular.TTF, 26)

	textC(img, kicker, "COMPUTER HELP  ·  DONVALE & MELBOURNE'S EAST", cx, 235, textMute)

	// Wordmark plus its brand-coloured full stop, centred as one unit.
	w := textWidth(title, "LOCAL IT HELP") + textWidth(title, ".")
	end := text(img, title, "LOCAL IT HELP", cx-w/2, 340, textPri)
	text(img, title, ".", end, 340, brand)

	textC(img, sub, "Friendly computer help, at your place.", cx, 400, textSec)
	textC(img, sub, "No fix, no fee  ·  Same or next day", cx, 442, textSec)
	textC(img, kicker, "localithelp.com.au", cx, 500, brand)

	if textWidth(title, "LOCAL IT HELP.") > safe {
		log.Fatalf("cover wordmark is %dpx wide, over the %dpx square-crop budget", w, safe)
	}
	return img
}

// squareImage is the 1:1 variant, for slots that crop to a square or near it —
// Google Business Profile photos being the reason it exists. Everything sits in
// the middle band vertically, so the usual 4:3 crop still shows the whole
// wordmark rather than lopping the ends off it.
func squareImage() *image.RGBA {
	const S = 1200
	img := image.NewRGBA(image.Rect(0, 0, S, S))
	fill(img, img.Bounds(), bgDeep)
	softGlow(img, 1040, 140, 460, accent, 0.18)
	softGlow(img, 140, 1050, 420, brand, 0.12)

	card := image.Rect(80, 80, S-80, S-80)
	roundedRect(img, card, 22, surface)
	fill(img, image.Rect(card.Min.X, card.Min.Y+40, card.Min.X+6, card.Max.Y-40), brand)

	kicker := face(gomono.TTF, 22)
	title := face(gobold.TTF, 84)
	sub := face(goregular.TTF, 32)
	small := face(goregular.TTF, 24)

	x := card.Min.X + 64
	text(img, kicker, "COMPUTER HELP  ·  DONVALE & MELBOURNE'S EAST", x, 340, textMute)

	end := text(img, title, "LOCAL IT HELP", x, 470, textPri)
	text(img, title, ".", end, 470, brand)

	text(img, sub, "Friendly computer help, at your place.", x, 545, textSec)
	text(img, sub, "Email · printers · Wi-Fi · scams · slow PCs · new setups", x, 595, textSec)
	text(img, sub, "— plus Shopify, websites & custom software.", x, 645, textSec)

	cx := x
	for _, c := range []string{"No fix, no fee", "Same or next day", "14-day guarantee"} {
		w := textWidth(small, c)
		roundedRect(img, image.Rect(cx, 730, cx+w+36, 772), 21, color.RGBA{0xee, 0xf2, 0xf7, 0xff})
		text(img, small, c, cx+18, 760, textPri)
		cx += w + 36 + 14
	}
	text(img, kicker, "localithelp.com.au", x, 850, brand)
	return img
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
