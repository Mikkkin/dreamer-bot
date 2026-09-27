package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// jpeg returns n bytes that the fake store accepts as a JPEG.
func jpeg(n int) []byte {
	return append([]byte{0xff, 0xd8}, bytes.Repeat([]byte{0x42}, n-2)...)
}

type formPart struct {
	field, filename string
	data            []byte
}

func multipartBody(t *testing.T, parts ...formPart) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		w, err := mw.CreateFormFile(p.field, p.filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(p.data)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func (h *harness) uploadParts(path string, user domain.UserID, parts ...formPart) *httptest.ResponseRecorder {
	h.t.Helper()
	body, ctype := multipartBody(h.t, parts...)
	return h.do(http.MethodPost, path, body, http.Header{"Authorization": {tma(user)}, "Content-Type": {ctype}})
}

func (h *harness) upload(path string, user domain.UserID, data []byte) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.uploadParts(path, user, formPart{field: "file", filename: "../../etc/passwd.jpg", data: data})
}

func TestUploadAndServeMedia(t *testing.T) {
	h := newHarness(t)
	id := createWish(t, h, alice, map[string]any{"title": "Камера"})
	data := jpeg(100)

	rec := h.upload("/api/wishes/"+id+"/images", alice, data)
	expectStatus(t, rec, http.StatusCreated)
	img := decode[imageJSON](t, rec)
	if img.ID == 0 || img.Width != 640 || img.Height != 800 {
		t.Fatalf("unexpected image %+v", img)
	}
	wantPrefix := "/media/" + strconv.FormatInt(img.ID, 10) + "/thumb?exp="
	if !strings.HasPrefix(img.ThumbURL, wantPrefix) || !strings.Contains(img.FullURL, "/full?exp=") {
		t.Fatalf("unexpected URLs %+v", img)
	}

	wish := decode[wishJSON](t, h.call(http.MethodGet, "/api/wishes/"+id, alice, nil))
	if len(wish.Images) != 1 || wish.Images[0].ThumbURL != img.ThumbURL {
		t.Fatalf("wish must list the image with the same stable URL: %+v", wish.Images)
	}

	for _, u := range []string{img.ThumbURL, img.FullURL} {
		rec = h.do(http.MethodGet, u, nil, nil)
		expectStatus(t, rec, http.StatusOK)
		if !bytes.Equal(rec.Body.Bytes(), data) {
			t.Fatal("served bytes differ")
		}
		hdr := rec.Header()
		if hdr.Get("Content-Type") != "image/jpeg" || hdr.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("unexpected headers %v", hdr)
		}
		if got := hdr.Get("Cache-Control"); got != "private, max-age=43200" {
			t.Fatalf("Cache-Control = %q", got)
		}
	}

	// Close to expiry the cache lifetime shrinks to what is left.
	h.clock.t = time.Date(2026, 9, 27, 22, 35, 0, 0, time.UTC)
	rec = h.do(http.MethodGet, img.ThumbURL, nil, nil)
	expectStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=1500" {
		t.Fatalf("Cache-Control = %q", got)
	}

	// After expiry the URL is refused.
	h.clock.t = time.Date(2026, 9, 27, 23, 0, 1, 0, time.UTC)
	rec = h.do(http.MethodGet, img.ThumbURL, nil, nil)
	expectError(t, rec, http.StatusForbidden, "forbidden", "")
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a refused media request must not be cached")
	}

	// Deleting the image.
	h.clock.t = testNow
	expectStatus(t, h.call(http.MethodDelete, "/api/wishes/"+id+"/images/"+strconv.FormatInt(img.ID, 10), alice, nil), http.StatusNoContent)
	expectError(t, h.call(http.MethodDelete, "/api/wishes/"+id+"/images/"+strconv.FormatInt(img.ID, 10), alice, nil), 404, "not_found", "")
}

func TestMediaRejectsBadSignatures(t *testing.T) {
	h := newHarness(t)
	id := createWish(t, h, alice, map[string]any{"title": "Камера"})
	img := decode[imageJSON](t, h.upload("/api/wishes/"+id+"/images", alice, jpeg(10)))
	u, _ := url.Parse(img.ThumbURL)
	q := u.Query()
	exp, sig := q.Get("exp"), q.Get("sig")
	imgID := strconv.FormatInt(img.ID, 10)
	other := "0" + sig[1:]
	if sig[0] == '0' {
		other = "1" + sig[1:]
	}

	forbidden := []string{
		"/media/" + imgID + "/thumb",
		"/media/" + imgID + "/thumb?exp=" + exp,
		"/media/" + imgID + "/thumb?exp=" + exp + "&sig=" + other,
		"/media/" + imgID + "/full?exp=" + exp + "&sig=" + sig,
		"/media/" + strconv.FormatInt(img.ID+1, 10) + "/thumb?exp=" + exp + "&sig=" + sig,
		"/media/" + imgID + "/thumb?exp=9" + exp + "&sig=" + sig,
	}
	for _, path := range forbidden {
		rec := h.do(http.MethodGet, path, nil, nil)
		expectError(t, rec, http.StatusForbidden, "forbidden", "")
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: refused media must not be cached", path)
		}
	}
	for _, path := range []string{"/media/abc/thumb", "/media/" + imgID + "/huge?exp=" + exp + "&sig=" + sig, "/media/" + imgID, "/media/"} {
		expectError(t, h.do(http.MethodGet, path, nil, nil), http.StatusNotFound, "not_found", "")
	}

	// A valid signature for an image that no longer exists.
	expectStatus(t, h.do(http.MethodGet, img.ThumbURL, nil, nil), http.StatusOK)
	delete(h.db.blobs, domain.ImageID(img.ID))
	expectError(t, h.do(http.MethodGet, img.ThumbURL, nil, nil), http.StatusNotFound, "not_found", "")
}

func TestUploadErrors(t *testing.T) {
	h := newHarness(t)
	id := createWish(t, h, alice, map[string]any{"title": "Камера"})
	path := "/api/wishes/" + id + "/images"

	t.Run("not multipart", func(t *testing.T) {
		expectError(t, h.call(http.MethodPost, path, alice, `{"file":"x"}`), 415, "unsupported_media", "")
	})
	t.Run("no parts", func(t *testing.T) {
		expectError(t, h.uploadParts(path, alice), 400, "validation", "file")
	})
	t.Run("wrong field", func(t *testing.T) {
		expectError(t, h.uploadParts(path, alice, formPart{field: "photo", filename: "a.jpg", data: jpeg(10)}), 400, "validation", "file")
	})
	t.Run("second part rolls back", func(t *testing.T) {
		rec := h.uploadParts(path, alice,
			formPart{field: "file", filename: "a.jpg", data: jpeg(10)},
			formPart{field: "file", filename: "b.jpg", data: jpeg(10)})
		expectError(t, rec, 400, "validation", "file")
		if len(h.db.removed) != 1 {
			t.Fatalf("the first image must be removed again, removed = %v", h.db.removed)
		}
		if w := decode[wishJSON](t, h.call(http.MethodGet, "/api/wishes/"+id, alice, nil)); len(w.Images) != 0 {
			t.Fatalf("wish keeps images of a rejected upload: %+v", w.Images)
		}
	})
	t.Run("too large", func(t *testing.T) {
		expectError(t, h.upload(path, alice, jpeg(maxImg+1)), 413, "too_large", "")
	})
	t.Run("unsupported format", func(t *testing.T) {
		expectError(t, h.upload(path, alice, []byte("\x89PNG\r\n\x1a\n....")), 415, "unsupported_media", "")
	})
	t.Run("missing wish", func(t *testing.T) {
		expectError(t, h.upload("/api/wishes/999/images", alice, jpeg(10)), 404, "not_found", "")
	})
	t.Run("limit", func(t *testing.T) {
		for range domain.MaxImagesPerWish {
			expectStatus(t, h.upload(path, alice, jpeg(10)), http.StatusCreated)
		}
		expectError(t, h.upload(path, alice, jpeg(10)), 422, "limit", "")
	})
	t.Run("recipe upload", func(t *testing.T) {
		rec := h.call(http.MethodPost, "/api/recipes", alice, map[string]string{"title": "Суп"})
		rid := strconv.FormatInt(decode[recipeJSON](t, rec).ID, 10)
		expectStatus(t, h.upload("/api/recipes/"+rid+"/images", alice, jpeg(10)), http.StatusCreated)
		r := decode[recipeJSON](t, h.call(http.MethodGet, "/api/recipes/"+rid, alice, nil))
		if len(r.Images) != 1 {
			t.Fatalf("recipe images: %+v", r.Images)
		}
		expectStatus(t, h.call(http.MethodDelete, "/api/recipes/"+rid+"/images/"+strconv.FormatInt(r.Images[0].ID, 10), alice, nil), http.StatusNoContent)
	})
	t.Run("anonymous upload", func(t *testing.T) {
		body, ctype := multipartBody(t, formPart{field: "file", filename: "a.jpg", data: jpeg(10)})
		expectError(t, h.do(http.MethodPost, path, body, http.Header{"Content-Type": {ctype}}), 401, "unauthorized", "")
	})
}
