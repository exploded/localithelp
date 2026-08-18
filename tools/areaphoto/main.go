// Command areaphoto prepares a photo for a suburb page:
//
//	go run ./tools/areaphoto -slug doncaster path/to/photo.jpg
//	go run ./tools/areaphoto -slug doncaster -anchor bottom path/to/photo.jpg   # keep the bottom of a tall shot
//
// It honours EXIF orientation (phone photos), centre-crops to 16:9, scales to
// 1200x675 (never upscales — smaller sources keep their width) and writes a
// tuned JPEG to static/img/areas/<slug>.jpg. That's all the suburb page needs;
// add/clear the Photo credit in cmd/server/suburbs.go if the source needs one.
//
// Review mode — tile every prepared image into one labelled sheet:
//
//	go run ./tools/areaphoto -sheet review.jpg
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	outW, outH = 1200, 675
	quality    = 80
)

func main() {
	slug := flag.String("slug", "", "suburb slug (output name)")
	dir := flag.String("dir", "static/img/areas", "output directory")
	sheet := flag.String("sheet", "", "write a labelled contact sheet of every image in -dir to this path and exit")
	anchor := flag.String("anchor", "centre", "which part of the source to keep when cropping: top|centre|bottom|left|right")
	flag.Parse()

	if *sheet != "" {
		if err := contactSheet(*dir, *sheet); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *slug == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: areaphoto -slug <slug> <photo.jpg|png>   |   areaphoto -sheet review.jpg")
		os.Exit(2)
	}
	if err := prepare(flag.Arg(0), filepath.Join(*dir, *slug+".jpg"), *anchor); err != nil {
		log.Fatal(err)
	}
}

func prepare(in, out, anchor string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	orient := exifOrientation(f)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode %s: %w", in, err)
	}
	src = applyOrientation(src, orient)

	// Centre-crop to 16:9.
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	cw, ch := w, w*outH/outW
	if ch > h {
		ch = h
		cw = h * outW / outH
	}
	ox, oy := (w-cw)/2, (h-ch)/2
	switch anchor {
	case "top":
		oy = 0
	case "bottom":
		oy = h - ch
	case "left":
		ox = 0
	case "right":
		ox = w - cw
	}
	crop := image.Rect(b.Min.X+ox, b.Min.Y+oy, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(cw, ch))

	tw, th := outW, outH
	if cw < outW { // never upscale
		tw, th = cw, ch
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	o, err := os.Create(out)
	if err != nil {
		return err
	}
	defer o.Close()
	if err := jpeg.Encode(o, dst, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	st, _ := o.Stat()
	fmt.Printf("%s  %dx%d  %dKB\n", out, tw, th, st.Size()/1024)
	return nil
}

// ── EXIF orientation (tag 0x0112) — enough to rotate phone photos correctly ──

func exifOrientation(r io.ReadSeeker) int {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil || hdr != [2]byte{0xFF, 0xD8} {
		return 1
	}
	for {
		var m [4]byte
		if _, err := io.ReadFull(r, m[:]); err != nil || m[0] != 0xFF {
			return 1
		}
		size := int(binary.BigEndian.Uint16(m[2:])) - 2
		if m[1] == 0xDA || size < 0 { // start of scan: no EXIF
			return 1
		}
		if m[1] != 0xE1 {
			if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
				return 1
			}
			continue
		}
		seg := make([]byte, size)
		if _, err := io.ReadFull(r, seg); err != nil {
			return 1
		}
		o, err := parseExifOrientation(seg)
		if err != nil {
			return 1
		}
		return o
	}
}

func parseExifOrientation(seg []byte) (int, error) {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 1, errors.New("no exif")
	}
	t := seg[6:]
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1, errors.New("bad tiff")
	}
	off := int(bo.Uint32(t[4:8]))
	if off+2 > len(t) {
		return 1, errors.New("bad ifd")
	}
	n := int(bo.Uint16(t[off:]))
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(t) {
			break
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			return int(bo.Uint16(t[e+8:])), nil
		}
	}
	return 1, nil
}

func applyOrientation(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.RGBA
	if o >= 5 {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, c)
		}
	}
	return dst
}

// ── contact sheet ──

func contactSheet(dir, out string) error {
	entries, err := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if err != nil {
		return err
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return errors.New("no images")
	}
	const cols, tw, th, pad, label = 4, 300, 169, 10, 22
	rows := (len(entries) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*(tw+pad)+pad, rows*(th+label+pad)+pad))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	ft, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return err
	}
	face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: 14, DPI: 72})
	if err != nil {
		return err
	}
	for i, p := range entries {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		x := pad + (i%cols)*(tw+pad)
		y := pad + (i/cols)*(th+label+pad)
		r := image.Rect(x, y, x+tw, y+th)
		xdraw.ApproxBiLinear.Scale(sheet, r, img, img.Bounds(), xdraw.Src, nil)
		d := &font.Drawer{Dst: sheet, Src: image.Black, Face: face, Dot: fixed.P(x, y+th+16)}
		d.DrawString(strings.TrimSuffix(filepath.Base(p), ".jpg"))
	}
	o, err := os.Create(out)
	if err != nil {
		return err
	}
	defer o.Close()
	return jpeg.Encode(o, sheet, &jpeg.Options{Quality: 75})
}
