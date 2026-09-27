package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

type imageService struct{ *core }

func (s imageService) Open(ctx context.Context, id domain.ImageID, v ImageVariant) (ImageFile, error) {
	if _, ok := ParseImageVariant(string(v)); !ok {
		return ImageFile{}, fmt.Errorf("image variant %q: %w", v, domain.ErrNotFound)
	}
	img, err := s.repos.GetImage(ctx, id)
	if err != nil {
		return ImageFile{}, err
	}
	return s.media.Open(img.Key, v)
}

// storeImage processes src into files and records them for owner. The
// caller has already checked the cheap preconditions so that no image is
// decoded for a missing or full owner; InsertImage re-checks them
// atomically. Files are removed again if the row cannot be inserted.
func (c *core) storeImage(ctx context.Context, owner ImageOwner, limit int, src io.Reader) (domain.Image, error) {
	stored, err := c.media.Save(ctx, src)
	if err != nil {
		return domain.Image{}, fmt.Errorf("store image: %w", err)
	}
	img, err := c.repos.InsertImage(ctx, owner, domain.Image{
		Key:       stored.Key,
		Width:     stored.Width,
		Height:    stored.Height,
		Bytes:     stored.Bytes,
		CreatedAt: c.now(),
	}, limit)
	if err != nil {
		c.deleteFiles(stored.Key)
		return domain.Image{}, err
	}
	c.log.Info("image added",
		slog.Int64("image_id", int64(img.ID)),
		slog.Int("width", img.Width),
		slog.Int("height", img.Height))
	return img, nil
}

// removeImage deletes the row first and the files after it, so the database
// never references a missing file.
func (c *core) removeImage(ctx context.Context, owner ImageOwner, id domain.ImageID) error {
	img, err := c.repos.DeleteImage(ctx, owner, id)
	if err != nil {
		return err
	}
	c.deleteFiles(img.Key)
	return nil
}
