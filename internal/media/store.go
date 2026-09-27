// Package media turns untrusted uploads into safe, normalized JPEG files and
// serves them back. Every upload is size-capped, format-allowlisted and
// pixel-capped before it is decoded, then re-encoded from pixels only, which
// drops EXIF (including GPS) and any payload hidden in the original file.
package media

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // registers the WebP decoder

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

var _ service.MediaStore = (*Store)(nil)

const (
	keyBytes    = 16
	keyLen      = 2 * keyBytes // hex
	jpegQuality = 85
	contentType = "image/jpeg"
	// maxConcurrentDecodes bounds memory: a 40 MP image needs several
	// hundred MB while it is decoded and resized, and imaging already uses
	// every core for a single image.
	maxConcurrentDecodes = 1
)

// Store keeps the variants of each image as
// {dir}/{key[:2]}/{key}_{variant}.jpg.
type Store struct {
	dir      string
	maxBytes int64
	slots    chan struct{}
}

// NewStore creates dir (mode 0700) if needed. maxBytes caps the size of an
// upload.
func NewStore(dir string, maxBytes int64) (*Store, error) {
	if dir == "" {
		return nil, errors.New("media: empty directory")
	}
	if maxBytes <= 0 {
		return nil, errors.New("media: maxBytes must be positive")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("media: resolve directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("media: create directory: %w", err)
	}
	return &Store{dir: abs, maxBytes: maxBytes, slots: make(chan struct{}, maxConcurrentDecodes)}, nil
}

// Save reads at most maxBytes from src, validates and re-encodes the image
// and writes its variants. It fails with domain.ErrImageTooLarge or
// domain.ErrImageUnsupported for unacceptable input.
func (s *Store) Save(ctx context.Context, src io.Reader) (service.StoredImage, error) {
	data, err := io.ReadAll(io.LimitReader(src, s.maxBytes+1))
	if err != nil {
		return service.StoredImage{}, fmt.Errorf("media: read upload: %w", err)
	}
	if int64(len(data)) > s.maxBytes {
		return service.StoredImage{}, fmt.Errorf("media: upload exceeds %d bytes: %w", s.maxBytes, domain.ErrImageTooLarge)
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return service.StoredImage{}, ctx.Err()
	}

	img, err := decode(data)
	if err != nil {
		return service.StoredImage{}, err
	}
	// Decoding can take a while; do not write files for a caller that is gone.
	if err := ctx.Err(); err != nil {
		return service.StoredImage{}, err
	}
	full, thumb := renderVariants(img)

	key, err := newKey()
	if err != nil {
		return service.StoredImage{}, err
	}
	size, err := s.writeVariants(key, full, thumb)
	if err != nil {
		return service.StoredImage{}, err
	}
	b := full.Bounds()
	return service.StoredImage{Key: key, Width: b.Dx(), Height: b.Dy(), Bytes: size}, nil
}

// Open returns one stored variant of key.
func (s *Store) Open(key string, v service.ImageVariant) (service.ImageFile, error) {
	p, err := s.path(key, v)
	if err != nil {
		return service.ImageFile{}, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return service.ImageFile{}, fmt.Errorf("media: image file: %w", domain.ErrNotFound)
	}
	if err != nil {
		return service.ImageFile{}, fmt.Errorf("media: open image: %w", err)
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = errors.New("not a regular file")
	}
	if err != nil {
		_ = f.Close()
		return service.ImageFile{}, fmt.Errorf("media: stat image: %w", err)
	}
	return service.ImageFile{Content: f, ContentType: contentType, ModTime: st.ModTime(), Size: st.Size()}, nil
}

// Delete removes every variant of key; missing files are ignored.
func (s *Store) Delete(key string) error {
	var errs []error
	for _, v := range [...]service.ImageVariant{service.VariantFull, service.VariantThumb} {
		p, err := s.path(key, v)
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("media: delete image: %w", err)
	}
	return nil
}

// path maps a key and a variant to a file path. The key must be exactly
// what newKey produces, which rules out path traversal by construction.
func (s *Store) path(key string, v service.ImageVariant) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("media: malformed image key: %w", domain.ErrNotFound)
	}
	if _, ok := service.ParseImageVariant(string(v)); !ok {
		return "", fmt.Errorf("media: unknown variant %q: %w", v, domain.ErrNotFound)
	}
	return s.file(key, v), nil
}

func (s *Store) file(key string, v service.ImageVariant) string {
	return filepath.Join(s.dir, key[:2], key+"_"+string(v)+".jpg")
}

func newKey() (string, error) {
	b := make([]byte, keyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("media: generate key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func validKey(key string) bool {
	if len(key) != keyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// writeVariants writes both files and returns the size of the full one. On
// failure nothing is left behind.
func (s *Store) writeVariants(key string, full, thumb image.Image) (int64, error) {
	if err := os.MkdirAll(filepath.Join(s.dir, key[:2]), 0o700); err != nil {
		return 0, fmt.Errorf("media: create directory: %w", err)
	}
	fullPath := s.file(key, service.VariantFull)
	size, err := writeJPEG(fullPath, full)
	if err != nil {
		return 0, err
	}
	if _, err := writeJPEG(s.file(key, service.VariantThumb), thumb); err != nil {
		_ = os.Remove(fullPath)
		return 0, err
	}
	return size, nil
}

// writeJPEG encodes img into a temporary file next to path and renames it
// into place, so a reader never sees a partial file.
func writeJPEG(path string, img image.Image) (size int64, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*.tmp") // mode 0600
	if err != nil {
		return 0, fmt.Errorf("media: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	w := bufio.NewWriter(tmp)
	if err := imaging.Encode(w, img, imaging.JPEG, imaging.JPEGQuality(jpegQuality)); err != nil {
		return 0, fmt.Errorf("media: encode jpeg: %w", err)
	}
	if err := w.Flush(); err != nil {
		return 0, fmt.Errorf("media: write image: %w", err)
	}
	st, err := tmp.Stat()
	if err != nil {
		return 0, fmt.Errorf("media: stat image: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return 0, fmt.Errorf("media: sync image: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("media: close image: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return 0, fmt.Errorf("media: rename image: %w", err)
	}
	return st.Size(), nil
}
