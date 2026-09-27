package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	stdjpeg "image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/media"
	"github.com/Mikkkin/dreamer-bot/internal/service"
	"github.com/Mikkkin/dreamer-bot/internal/storage/sqlite"
)

// TestRealStack drives the handler over the real use cases, SQLite and the
// media store, to catch contract drift between the layers.
func TestRealStack(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	dir := t.TempDir()
	quiet := slog.New(slog.DiscardHandler)
	db, err := sqlite.Open(ctx, filepath.Join(dir, "dreamer.db"), quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := media.NewStore(filepath.Join(dir, "images"), 5<<20)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(service.Deps{Repos: db, Media: store, Whitelist: []domain.UserID{alice, bob}, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(Options{
		Services:        svc,
		Validator:       auth.NewInitDataValidator(testToken, 24*time.Hour, nil),
		Whitelist:       auth.NewWhitelist([]domain.UserID{alice, bob}),
		Signer:          auth.NewMediaSigner(testToken, nil),
		DefaultCurrency: "EUR",
		MaxImageBytes:   5 << 20,
		Log:             quiet,
		Health:          db.Ping,
	}))
	defer srv.Close()

	call := func(method, path string, user domain.UserID, body io.Reader, ctype string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if user != 0 {
			req.Header.Set("Authorization", "tma "+signInitData(testToken, user, time.Now()))
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	jsonCall := func(method, path string, body string, want int, out any) {
		t.Helper()
		var r io.Reader
		ctype := ""
		if body != "" {
			r, ctype = strings.NewReader(body), "application/json"
		}
		resp := call(method, path, alice, r, ctype)
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, data)
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				t.Fatalf("%s %s: %v: %s", method, path, err, data)
			}
		}
	}

	if resp := call(http.MethodGet, "/healthz", 0, nil, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	var categories struct {
		Categories []categoryJSON `json:"categories"`
	}
	jsonCall(http.MethodGet, "/api/categories", "", http.StatusOK, &categories)
	if len(categories.Categories) != len(domain.DefaultCategories()) {
		t.Fatalf("default categories not seeded: %+v", categories)
	}
	catID := strconv.FormatInt(categories.Categories[1].ID, 10)

	var wish wishJSON
	jsonCall(http.MethodPost, "/api/wishes",
		`{"title":"Поездка в Токио","category_id":`+catID+`,"link":"example.com","price":{"amount":"1 200,50","currency":"EUR"}}`,
		http.StatusCreated, &wish)
	if wish.Author.Name != "Алиса" || wish.Price == nil || wish.Price.Amount != "1200.50" {
		t.Fatalf("unexpected wish %+v", wish)
	}
	wishPath := "/api/wishes/" + strconv.FormatInt(wish.ID, 10)

	var patched wishJSON
	jsonCall(http.MethodPatch, wishPath, `{"link":null,"hot":true}`, http.StatusOK, &patched)
	if patched.Link != nil || !patched.Hot || patched.Price == nil {
		t.Fatalf("patch: %+v", patched)
	}
	jsonCall(http.MethodPatch, wishPath, `{"category_id":987654}`, http.StatusBadRequest, nil)

	// Upload a real JPEG through multipart and fetch the signed thumbnail.
	var photo bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for x := range 800 {
		src.Set(x, x%600, color.RGBA{R: 200, A: 255})
	}
	if err := stdjpeg.Encode(&photo, src, nil); err != nil {
		t.Fatal(err)
	}
	body, ctype := multipartBody(t, formPart{field: "file", filename: "photo.jpg", data: photo.Bytes()})
	resp := call(http.MethodPost, wishPath+"/images", alice, body, ctype)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d %s", resp.StatusCode, data)
	}
	var img imageJSON
	if err := json.Unmarshal(data, &img); err != nil {
		t.Fatal(err)
	}
	thumb := call(http.MethodGet, img.ThumbURL, 0, nil, "")
	if thumb.StatusCode != http.StatusOK || thumb.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb: %d %v", thumb.StatusCode, thumb.Header)
	}
	cfg, err := stdjpeg.DecodeConfig(thumb.Body)
	if err != nil || cfg.Width != 480 || cfg.Height != 600 {
		t.Fatalf("thumb is not a 480x600 JPEG: %+v %v", cfg, err)
	}
	notImage, ntype := multipartBody(t, formPart{field: "file", filename: "x.gif", data: []byte("GIF89a....")})
	if resp := call(http.MethodPost, wishPath+"/images", alice, notImage, ntype); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image upload: %d", resp.StatusCode)
	}

	jsonCall(http.MethodPut, wishPath+"/status", `{"status":"done"}`, http.StatusOK, &patched)
	var stats map[string]json.RawMessage
	jsonCall(http.MethodGet, "/api/stats", "", http.StatusOK, &stats)
	if !strings.Contains(string(stats["overall"]), `"done":{"count":1`) {
		t.Fatalf("stats: %s", stats["overall"])
	}

	var recipe recipeJSON
	jsonCall(http.MethodGet, "/api/recipes/random", "", http.StatusNotFound, nil)
	jsonCall(http.MethodPost, "/api/recipes", `{"title":"Борщ","body":"Свёкла"}`, http.StatusCreated, &recipe)
	jsonCall(http.MethodGet, "/api/recipes/random", "", http.StatusOK, &recipe)

	var me meJSON
	if resp := call(http.MethodGet, "/api/me", bob, nil, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("bob /api/me: %d", resp.StatusCode)
	}
	jsonCall(http.MethodGet, "/api/me", "", http.StatusOK, &me)
	if me.User.Name != "Алиса" || len(me.Partners) != 1 || me.Partners[0].Name != "Боб" {
		t.Fatalf("me: %+v", me)
	}

	jsonCall(http.MethodDelete, wishPath, "", http.StatusNoContent, nil)
	if resp := call(http.MethodGet, img.ThumbURL, 0, nil, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("image of a deleted wish: %d", resp.StatusCode)
	}
}
