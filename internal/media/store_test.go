package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/image/bmp"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const testMaxBytes = 10 << 20

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "media"), testMaxBytes)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// gradient is an opaque test picture with some structure so that the
// encoders produce realistic output.
func gradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// withEXIFOrientation inserts a minimal big-endian EXIF APP1 segment that
// carries only the orientation tag right after the JPEG SOI marker. It also
// carries a fake GPS-like marker string to prove metadata is not copied.
func withEXIFOrientation(t *testing.T, jpg []byte, orientation uint16) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatal("not a JPEG")
	}
	var tiff bytes.Buffer
	tiff.WriteString("MM") // big-endian
	_ = binary.Write(&tiff, binary.BigEndian, uint16(42))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8)) // offset of IFD0
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1)) // one entry
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3)) // SHORT
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, orientation)
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0)) // padding
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0)) // no next IFD
	tiff.WriteString("GPS-SECRET-52.37N")

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var seg bytes.Buffer
	seg.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&seg, binary.BigEndian, uint16(len(payload)+2))
	seg.Write(payload)

	out := append([]byte{0xFF, 0xD8}, seg.Bytes()...)
	return append(out, jpg[2:]...)
}

// pngHeader returns a PNG signature and a valid IHDR chunk claiming w×h
// RGBA pixels, followed by nothing: DecodeConfig accepts it, a full decode
// would have to allocate w*h*4 bytes.
func pngHeader(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit RGBA, deflate, no filter, no interlace

	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(13))
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}

// Tiny valid WebP files (1×1) in the three container flavours.
var webpFixtures = map[string]string{
	"lossless": "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==",
	"lossy":    "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA",
	"alpha":    "UklGRkoAAABXRUJQVlA4WAoAAAAQAAAAAAAAAAAAQUxQSAwAAAARBxAR/Q9ERP8DAABWUDggGAAAABQBAJ0BKgEAAQAAAP4AAA3AAP7mtQAAAA==",
}

func readVariant(t *testing.T, s *Store, key string, v service.ImageVariant) ([]byte, image.Image) {
	t.Helper()
	f, err := s.Open(key, v)
	if err != nil {
		t.Fatalf("Open(%s): %v", v, err)
	}
	defer f.Content.Close()
	if f.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q", f.ContentType)
	}
	data, err := io.ReadAll(f.Content)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if int64(len(data)) != f.Size {
		t.Errorf("Size = %d, read %d bytes", f.Size, len(data))
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		t.Fatalf("stored %s is not a JPEG (%q): %v", v, format, err)
	}
	return data, img
}

func size(img image.Image) image.Point { return img.Bounds().Size() }

func TestSaveAppliesEXIFOrientationAndStripsMetadata(t *testing.T) {
	s := newTestStore(t)
	src := withEXIFOrientation(t, encodeJPEG(t, gradient(200, 100)), 6)

	got, err := s.Save(context.Background(), bytes.NewReader(src))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got.Width != 100 || got.Height != 200 {
		t.Errorf("stored %dx%d, want the rotated 100x200", got.Width, got.Height)
	}
	full, img := readVariant(t, s, got.Key, service.VariantFull)
	if size(img) != image.Pt(100, 200) {
		t.Errorf("full variant is %v, want 100x200", size(img))
	}
	if got.Bytes != int64(len(full)) {
		t.Errorf("Bytes = %d, full file has %d", got.Bytes, len(full))
	}
	for _, v := range []service.ImageVariant{service.VariantFull, service.VariantThumb} {
		data, _ := readVariant(t, s, got.Key, v)
		if bytes.Contains(data, []byte("Exif")) || bytes.Contains(data, []byte("GPS-SECRET")) {
			t.Errorf("%s variant still carries metadata", v)
		}
	}
}

func TestSaveVariantsSizes(t *testing.T) {
	cases := []struct {
		name string
		src  func(t *testing.T) []byte
		full image.Point
	}{
		{"tall screenshot keeps its width", func(t *testing.T) []byte { return encodePNG(t, gradient(1170, 2532)) }, image.Pt(1170, 2532)},
		{"wide photo is scaled to 1600", func(t *testing.T) []byte { return encodeJPEG(t, gradient(3200, 1000)) }, image.Pt(1600, 500)},
		{"small image is not upscaled", func(t *testing.T) []byte { return encodeJPEG(t, gradient(300, 200)) }, image.Pt(300, 200)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			got, err := s.Save(context.Background(), bytes.NewReader(tc.src(t)))
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if got.Width != tc.full.X || got.Height != tc.full.Y {
				t.Errorf("stored %dx%d, want %v", got.Width, got.Height, tc.full)
			}
			if _, img := readVariant(t, s, got.Key, service.VariantFull); size(img) != tc.full {
				t.Errorf("full variant is %v, want %v", size(img), tc.full)
			}
			if _, img := readVariant(t, s, got.Key, service.VariantThumb); size(img) != image.Pt(thumbWidth, thumbHeight) {
				t.Errorf("thumb variant is %v, want %dx%d", size(img), thumbWidth, thumbHeight)
			}
		})
	}
}

func TestSaveFlattensTransparencyOnWhite(t *testing.T) {
	s := newTestStore(t)
	src := image.NewNRGBA(image.Rect(0, 0, 64, 64)) // fully transparent black
	got, err := s.Save(context.Background(), bytes.NewReader(encodePNG(t, src)))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, v := range []service.ImageVariant{service.VariantFull, service.VariantThumb} {
		_, img := readVariant(t, s, got.Key, v)
		r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
		if r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
			t.Errorf("%s: transparent pixel became (%d,%d,%d), want white", v, r>>8, g>>8, b>>8)
		}
	}
}

func TestSaveDecodesWebP(t *testing.T) {
	for name, b64 := range webpFixtures {
		t.Run(name, func(t *testing.T) {
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				t.Fatal(err)
			}
			s := newTestStore(t)
			got, err := s.Save(context.Background(), bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if got.Width != 1 || got.Height != 1 {
				t.Errorf("stored %dx%d, want 1x1", got.Width, got.Height)
			}
			readVariant(t, s, got.Key, service.VariantThumb)
		})
	}
}

func TestSaveRejects(t *testing.T) {
	encodeWith := func(enc func(io.Writer, image.Image) error) []byte {
		var buf bytes.Buffer
		if err := enc(&buf, gradient(8, 8)); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gifEnc := func(w io.Writer, img image.Image) error { return gif.Encode(w, img, nil) }

	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"text", []byte("definitely not an image"), domain.ErrImageUnsupported},
		{"empty", nil, domain.ErrImageUnsupported},
		{"html with image magic later", []byte("<html>\x89PNG</html>"), domain.ErrImageUnsupported},
		{"bmp", encodeWith(bmp.Encode), domain.ErrImageUnsupported},
		{"gif", encodeWith(gifEnc), domain.ErrImageUnsupported},
		{"truncated png", encodePNG(t, gradient(64, 64))[:100], domain.ErrImageUnsupported},
		// The header has no pixel data: if it were decoded, the error would be
		// ErrImageUnsupported, so ErrImageTooLarge proves the early rejection.
		{"decompression bomb", pngHeader(20000, 20000), domain.ErrImageTooLarge},
		{"side beyond JPEG limit", pngHeader(70000, 10), domain.ErrImageTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			_, err := s.Save(context.Background(), bytes.NewReader(tc.data))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Save error = %v, want %v", err, tc.want)
			}
			assertNoFiles(t, s)
		})
	}
}

func TestSaveRejectsOversizedUpload(t *testing.T) {
	s, err := NewStore(t.TempDir(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0xFF}, 1001)
	if _, err := s.Save(context.Background(), bytes.NewReader(payload)); !errors.Is(err, domain.ErrImageTooLarge) {
		t.Fatalf("Save error = %v, want ErrImageTooLarge", err)
	}

	// The reader must be capped, not drained: an endless stream still fails fast.
	if _, err := s.Save(context.Background(), endless{}); !errors.Is(err, domain.ErrImageTooLarge) {
		t.Fatalf("Save(endless) error = %v, want ErrImageTooLarge", err)
	}
}

type endless struct{}

func (endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestSaveHonoursCancelledContext(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Save(ctx, bytes.NewReader(encodeJPEG(t, gradient(32, 32)))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save error = %v, want context.Canceled", err)
	}
	assertNoFiles(t, s)
}

func TestSaveConcurrent(t *testing.T) {
	s := newTestStore(t)
	src := encodeJPEG(t, gradient(120, 90))
	var wg sync.WaitGroup
	keys := make([]string, 6)
	errs := make([]error, len(keys))
	for i := range keys {
		wg.Go(func() {
			got, err := s.Save(context.Background(), bytes.NewReader(src))
			keys[i], errs[i] = got.Key, err
		})
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		if seen[keys[i]] {
			t.Fatalf("duplicate key %s", keys[i])
		}
		seen[keys[i]] = true
	}
}

func TestFilesArePrivateAndDeleteRemovesThem(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Save(context.Background(), bytes.NewReader(encodeJPEG(t, gradient(40, 40))))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !validKey(got.Key) {
		t.Fatalf("key %q is not 32 lowercase hex characters", got.Key)
	}
	for _, v := range []string{"full", "thumb"} {
		p := filepath.Join(s.dir, got.Key[:2], got.Key+"_"+v+".jpg")
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", v, err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s file mode = %o, want 600", v, perm)
		}
	}
	for _, d := range []string{s.dir, filepath.Join(s.dir, got.Key[:2])} {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o700 {
			t.Errorf("directory %s mode = %o, want 700", d, perm)
		}
	}

	if err := s.Delete(got.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	assertNoFiles(t, s)
	if err := s.Delete(got.Key); err != nil {
		t.Fatalf("second Delete must ignore missing files: %v", err)
	}
	if _, err := s.Open(got.Key, service.VariantFull); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Open after Delete = %v, want ErrNotFound", err)
	}
}

func TestOpenAndDeleteRejectMalformedKeys(t *testing.T) {
	s := newTestStore(t)
	// A file outside the store that a traversal would reach.
	secret := filepath.Join(filepath.Dir(s.dir), "secret_full.jpg")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys := []string{
		"",
		"../secret",
		"../../../../etc/passwd",
		"0123456789ABCDEF0123456789ABCDEF",    // upper case
		"0123456789abcdef0123456789abcde",     // too short
		"0123456789abcdef0123456789abcdef0",   // too long
		"0123456789abcdef0123456789abcde/",    // separator
		"0123456789abcdef0123456789abcd\x00f", // NUL
	}
	for _, key := range keys {
		if _, err := s.Open(key, service.VariantFull); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Open(%q) = %v, want ErrNotFound", key, err)
		}
		if err := s.Delete(key); err == nil {
			t.Errorf("Delete(%q) succeeded", key)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("file outside the store was touched: %v", err)
	}
	valid := strings.Repeat("ab", keyBytes)
	if _, err := s.Open(valid, "original"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Open with unknown variant = %v, want ErrNotFound", err)
	}
}

func TestNewStoreValidates(t *testing.T) {
	if _, err := NewStore("", 1); err == nil {
		t.Error("empty dir accepted")
	}
	if _, err := NewStore(t.TempDir(), 0); err == nil {
		t.Error("zero maxBytes accepted")
	}
}

func TestFitFull(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{1170, 2532, 1170, 2532},
		{1600, 7500, 1600, 7500}, // exactly 12 MP
		{4000, 3000, 1600, 1200},
		{3000, 4000, 1600, 2133},
		{1600, 8000, 1549, 7745},
		{1000, 20000, 774, 15491},
		{1, 40_000_000, 1, 12_000_000},
		{1, 1, 1, 1},
	}
	for _, c := range cases {
		w, h := fitFull(c.w, c.h)
		if w != c.wantW || h != c.wantH {
			t.Errorf("fitFull(%d, %d) = %dx%d, want %dx%d", c.w, c.h, w, h, c.wantW, c.wantH)
		}
		if int64(w)*int64(h) > fullMaxPixels || w > fullMaxWidth {
			t.Errorf("fitFull(%d, %d) = %dx%d exceeds the limits", c.w, c.h, w, h)
		}
	}
}

func TestCoverHandlesExtremeAspectRatios(t *testing.T) {
	for _, src := range []image.Point{{1, 30000}, {30000, 1}, {50, 50}, {480, 600}, {2000, 100}} {
		out := cover(gradient(src.X, src.Y), thumbWidth, thumbHeight)
		if size(out) != image.Pt(thumbWidth, thumbHeight) {
			t.Errorf("cover(%v) = %v, want %dx%d", src, size(out), thumbWidth, thumbHeight)
		}
	}
}

func TestCoverKeepsTopOfScreenshots(t *testing.T) {
	// A 1000×3000 "screenshot": the first 500 rows are red (the recipe
	// title), the rest blue. A centered crop would show only blue.
	src := image.NewRGBA(image.Rect(0, 0, 1000, 3000))
	for y := range 3000 {
		c := color.RGBA{B: 255, A: 255}
		if y < 500 {
			c = color.RGBA{R: 255, A: 255}
		}
		for x := range 1000 {
			src.SetRGBA(x, y, c)
		}
	}
	r, g, b, _ := cover(src, thumbWidth, thumbHeight).At(thumbWidth/2, 10).RGBA()
	if r>>8 < 200 || g>>8 > 50 || b>>8 > 50 {
		t.Fatalf("screenshot cover must keep the top rows, got rgb(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func assertNoFiles(t *testing.T, s *Store) {
	t.Helper()
	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("unexpected file left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
