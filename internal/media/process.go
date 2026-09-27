package media

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/disintegration/imaging"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const (
	// maxSourcePixels rejects decompression bombs before any pixel buffer
	// is allocated.
	maxSourcePixels = 40_000_000
	// maxSourceSide is the largest dimension JPEG can encode; since the
	// full variant is only ever scaled down, it always stays encodable.
	maxSourceSide = 65_535

	thumbWidth  = 480
	thumbHeight = 600 // 4:5, the ratio of a card cover

	fullMaxWidth  = 1600
	fullMaxPixels = 12_000_000
)

// decode validates data and decodes it with the EXIF orientation applied.
// imaging registers BMP, TIFF and GIF decoders globally, so the format is
// allowlisted from the header before any pixel data is touched.
func decode(data []byte) (img image.Image, err error) {
	// The decoders parse untrusted bytes; contain a decoder bug to this
	// upload instead of the request or bot handler.
	defer func() {
		if p := recover(); p != nil {
			img, err = nil, fmt.Errorf("media: decoder panic: %v: %w", p, domain.ErrImageUnsupported)
		}
	}()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("media: read image header: %v: %w", err, domain.ErrImageUnsupported)
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return nil, fmt.Errorf("media: format %q: %w", format, domain.ErrImageUnsupported)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("media: empty image: %w", domain.ErrImageUnsupported)
	}
	if cfg.Width > maxSourceSide || cfg.Height > maxSourceSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxSourcePixels {
		return nil, fmt.Errorf("media: %dx%d pixels: %w", cfg.Width, cfg.Height, domain.ErrImageTooLarge)
	}
	img, err = imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("media: decode %s: %v: %w", format, err, domain.ErrImageUnsupported)
	}
	return img, nil
}

// renderVariants produces the opaque full and thumb renditions of img.
func renderVariants(img image.Image) (full, thumb image.Image) {
	b := img.Bounds()
	full = img
	if w, h := fitFull(b.Dx(), b.Dy()); w != b.Dx() || h != b.Dy() {
		full = imaging.Resize(img, w, h, imaging.Lanczos)
	}
	return flatten(full), flatten(cover(img, thumbWidth, thumbHeight))
}

// fitFull returns the size of the full variant: scaled down, keeping the
// aspect ratio, only when the image is wider than fullMaxWidth or larger
// than fullMaxPixels. Tall screenshots therefore keep their width and stay
// readable.
func fitFull(w, h int) (int, int) {
	if w > fullMaxWidth {
		h = max(1, int(math.Round(float64(h)*fullMaxWidth/float64(w))))
		w = fullMaxWidth
	}
	if int64(w)*int64(h) > fullMaxPixels {
		scale := math.Sqrt(fullMaxPixels / (float64(w) * float64(h)))
		w = max(1, int(float64(w)*scale))
		h = max(1, int(float64(h)*scale))
		// Guard against floating-point rounding and the 1 px clamp above.
		if int64(w)*int64(h) > fullMaxPixels {
			h = fullMaxPixels / w
		}
	}
	return w, h
}

// screenshotAspect is the height/width ratio above which an image is treated
// as a phone screenshot (e.g. 1170×2532 ≈ 2.16): its cover keeps the top,
// where a recipe's title and ingredients are, instead of the middle.
const screenshotAspect = 1.6

// cover scales and crops img to exactly w×h: centered for photos, anchored
// at the top for screenshot-like images. It crops before it resizes:
// imaging.Fill resizes first for sources under 100 px on a side, so a
// 1×30000 strip would be blown up to 480×14,400,000 pixels.
func cover(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	cw, ch := sw, sh
	if int64(sw)*int64(h) > int64(sh)*int64(w) {
		cw = max(1, int(math.Round(float64(sh)*float64(w)/float64(h))))
	} else {
		ch = max(1, int(math.Round(float64(sw)*float64(h)/float64(w))))
	}
	anchor := imaging.Center
	if float64(sh) > screenshotAspect*float64(sw) {
		anchor = imaging.Top
	}
	cropped := imaging.CropAnchor(img, min(cw, sw), min(ch, sh), anchor)
	return imaging.Resize(cropped, w, h, imaging.Lanczos)
}

// flatten composites img onto white. JPEG has no alpha channel, and the
// encoder would otherwise turn transparent areas black.
func flatten(img image.Image) image.Image {
	if o, ok := img.(interface{ Opaque() bool }); ok && o.Opaque() {
		return img
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}
