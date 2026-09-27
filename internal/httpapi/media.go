package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

const (
	// uploadOverhead is the multipart framing allowed on top of the image.
	uploadOverhead = 1 << 20
	// maxMediaAge caps the browser cache lifetime of an image response at
	// the minimum lifetime of a signed URL.
	maxMediaAge = 12 * 60 * 60
	uploadField = "file"
	// uploadDeadline and mediaWriteDeadline extend the server-wide 60s
	// timeouts for the two slow routes: photo uploads over weak mobile links
	// (the Mini App waits up to 180s) and full-size image downloads.
	uploadDeadline     = 3 * time.Minute
	mediaWriteDeadline = 2 * time.Minute
)

// media serves GET /media/{id}/{variant}?exp=&sig=. The signed URL is the
// credential, because <img> cannot send the Authorization header.
func (s *server) media(w http.ResponseWriter, r *http.Request) {
	id, okID := parseID(r.PathValue("id"))
	variant, okVariant := service.ParseImageVariant(r.PathValue("variant"))
	if !okID || !okVariant {
		writeError(w, errNotFound)
		return
	}
	q := r.URL.Query()
	exp, sig := q.Get("exp"), q.Get("sig")
	if !s.signer.Verify(domain.ImageID(id), string(variant), exp, sig) {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, errMediaForbidden)
		return
	}
	file, err := s.svc.Images.Open(r.Context(), domain.ImageID(id), variant)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer file.Content.Close()

	expUnix, _ := strconv.ParseInt(exp, 10, 64) // Verify accepted it as canonical
	maxAge := max(0, min(expUnix-s.now().Unix(), maxMediaAge))
	extendDeadline(w, 0, mediaWriteDeadline)
	h := w.Header()
	h.Set("Content-Type", file.ContentType)
	h.Set("Cache-Control", "private, max-age="+strconv.FormatInt(maxAge, 10))
	http.ServeContent(w, r, "", file.ModTime, file.Content)
}

// receiveImage streams the single multipart part named "file" into add.
// The request is all-or-nothing: if anything follows that part, the stored
// image is removed again with undo.
func (s *server) receiveImage(
	w http.ResponseWriter,
	r *http.Request,
	add func(io.Reader) (domain.Image, error),
	undo func(domain.ImageID) error,
) error {
	extendDeadline(w, uploadDeadline, uploadDeadline)
	r.Body = http.MaxBytesReader(w, r.Body, s.maxImage+uploadOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return errNotMultipart
	}
	part, err := mr.NextPart()
	switch {
	case errors.Is(err, io.EOF):
		return badRequest(uploadField, "Прикрепите фото.")
	case err != nil:
		return multipartError(err)
	case part.FormName() != uploadField:
		return badRequest(uploadField, "Фото нужно отправить в поле file.")
	}
	img, err := add(part)
	if err != nil {
		return err
	}
	if _, err := mr.NextPart(); !errors.Is(err, io.EOF) {
		if undoErr := undo(img.ID); undoErr != nil {
			s.log.ErrorContext(r.Context(), "remove image of a rejected upload", "image_id", int64(img.ID), "err", undoErr)
		}
		if err != nil {
			return multipartError(err)
		}
		return badRequest(uploadField, "Отправьте ровно одно фото.")
	}
	writeJSON(w, http.StatusCreated, imageOf(s.signer, img))
	return nil
}

func multipartError(err error) error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return err
	}
	return badRequest(uploadField, "Не удалось прочитать загрузку.")
}

// extendDeadline replaces the server-wide read/write deadlines for the current
// request; a zero duration keeps the server default. An error only means the
// ResponseWriter cannot set deadlines (httptest recorders), where the default
// is fine, so it is deliberately ignored.
func extendDeadline(w http.ResponseWriter, read, write time.Duration) {
	rc := http.NewResponseController(w)
	now := time.Now()
	if read > 0 {
		_ = rc.SetReadDeadline(now.Add(read))
	}
	if write > 0 {
		_ = rc.SetWriteDeadline(now.Add(write))
	}
}
