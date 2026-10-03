package recipeimport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// fakeVideoLLM reads captions like fakeLLM and videos with a canned
// answer.
type fakeVideoLLM struct {
	fakeLLM
	video      Parsed
	videoErr   error
	videoDelay time.Duration

	mu         sync.Mutex
	videoCalls int
	gotVideo   []byte
	gotMime    string
	gotCaption string
}

func (f *fakeVideoLLM) ParseVideo(ctx context.Context, video []byte, mime, caption string) (Parsed, error) {
	f.mu.Lock()
	f.videoCalls++
	f.gotVideo, f.gotMime, f.gotCaption = video, mime, caption
	f.mu.Unlock()
	if f.videoDelay > 0 {
		select {
		case <-time.After(f.videoDelay):
		case <-ctx.Done():
			return Parsed{}, ErrLLMUnavailable
		}
	}
	return f.video, f.videoErr
}

func mustQty(t *testing.T, amount, unit string) *domain.Quantity {
	t.Helper()
	q, err := domain.ParseQuantity(amount, unit)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// instagram serves posts (path → caption), their embed pages (with a
// video for the shortcodes in videos) and the videos on the CDN.
type instagram struct {
	captions map[string]string
	videos   map[string]bool
	mu       sync.Mutex
	embeds   int
	cdn      int
}

func (ig *instagram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Host == "scontent.cdninstagram.com" && strings.HasSuffix(path, ".mp4"):
		ig.mu.Lock()
		ig.cdn++
		ig.mu.Unlock()
		if strings.HasPrefix(path, "/FORBIDDEN") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(fakeMP4)
	case r.Host != canonicalHost:
		http.Error(w, "unexpected host", http.StatusTeapot)
	case strings.HasSuffix(path, "/embed/captioned/"):
		ig.mu.Lock()
		ig.embeds++
		ig.mu.Unlock()
		code := strings.Split(strings.Trim(path, "/"), "/")[1]
		if !ig.videos[code] {
			_, _ = w.Write([]byte(`<script>{"contextJSON":"{\"context\":{\"is_video\":false}}"}</script>`))
			return
		}
		_, _ = w.Write([]byte(embedPage(`https:\\\/\\\/scontent.cdninstagram.com\\\/` + code + `.mp4`)))
	default:
		caption, ok := ig.captions[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		desc := `12 likes, 1 comment - cook on March 1, 2026: "` + caption + `".`
		if caption == "" {
			desc = `12 likes, 1 comment - cook on March 1, 2026`
		}
		_, _ = w.Write([]byte(postPage(desc, "")))
	}
}

func newInstagram(t *testing.T) (*instagram, *Fetcher) {
	ig := &instagram{
		captions: map[string]string{
			"/reel/LOWCAPT01/": lowCaption,
			"/reel/LOWCAPT02/": lowCaption,
			"/reel/NOCAPTION/": "",
			"/reel/MORKOVKA1/": readFixture(t, "03-morkovka-po-korejski"),
			"/reel/KEKSY0001/": readFixture(t, "10-keksy-na-kefire"),
			"/reel/NOTRECIPE/": readFixture(t, "34-negative-igra-gryaz"),
			"/reel/FORBIDDEN/": lowCaption,
			"/p/PHOTO0001/":    lowCaption,
			"/p/NOTRECIPE/":    readFixture(t, "34-negative-igra-gryaz"),
		},
		videos: map[string]bool{"LOWCAPT01": true, "LOWCAPT02": true, "NOCAPTION": true, "MORKOVKA1": true, "KEKSY0001": true, "NOTRECIPE": true, "FORBIDDEN": true},
	}
	srv := httptest.NewServer(ig)
	t.Cleanup(srv.Close)
	return ig, testFetcher(srv, nil)
}

func videoRecipe(t *testing.T) Parsed {
	return Parsed{
		Title: "Борщ",
		Ingredients: []domain.Ingredient{
			{Name: "Свекла", Quantity: mustQty(t, "2", "шт")},
			{Name: "Капуста", Quantity: mustQty(t, "300", "г")},
			{Name: "Соль", Quantity: mustQty(t, "", "по вкусу")},
		},
		Steps:      []string{"Натереть свеклу.", "Обжарить с морковью.", "Варить с капустой 20 минут."},
		Confidence: 0.85,
	}
}

func TestFromURLReadsTheVideo(t *testing.T) {
	ig, f := newInstagram(t)
	llm := &fakeVideoLLM{video: videoRecipe(t)}
	im := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}
	res, err := im.FromURL(context.Background(), "https://www.instagram.com/reel/LOWCAPT01/?igsh=x")
	if err != nil {
		t.Fatal(err)
	}
	mustBeValidDraft(t, res.Draft)
	d, r := res.Draft, res.Report
	if r.Parser != ParserVideo || r.Source != SourceInstagram || r.Confidence != 0.85 || len(r.Warnings) != 0 {
		t.Errorf("report = %+v", r)
	}
	if d.Title != "Борщ" || len(d.Ingredients) != 3 || d.Body != "1. Натереть свеклу.\n2. Обжарить с морковью.\n3. Варить с капустой 20 минут." {
		t.Errorf("draft = %+v", d)
	}
	if d.Link == nil || *d.Link != "https://www.instagram.com/reel/LOWCAPT01/" {
		t.Errorf("link = %v", d.Link)
	}
	if llm.videoCalls != 1 || llm.calls != 0 {
		t.Errorf("video calls %d, caption calls %d; want only the video read", llm.videoCalls, llm.calls)
	}
	if string(llm.gotVideo) != string(fakeMP4) || llm.gotMime != "video/mp4" || llm.gotCaption != lowCaption {
		t.Errorf("ParseVideo got %d bytes of %q with caption %q", len(llm.gotVideo), llm.gotMime, llm.gotCaption)
	}
	if ig.embeds != 1 || ig.cdn != 1 {
		t.Errorf("%d embed pages, %d videos fetched", ig.embeds, ig.cdn)
	}
	if im.Videos.used != 1 {
		t.Errorf("quota used = %d", im.Videos.used)
	}

	// A reel without any caption is read from the video alone.
	res, err = im.FromURL(context.Background(), "https://www.instagram.com/reel/NOCAPTION/")
	if err != nil || res.Report.Parser != ParserVideo || res.Draft.Title != "Борщ" || llm.gotCaption != "" {
		t.Errorf("no caption: %+v, %v", res.Report, err)
	}
}

func TestFromURLVideoAddsStepsToACaptionList(t *testing.T) {
	_, f := newInstagram(t)
	fewer := videoRecipe(t)
	fewer.Title = "Морковча"
	llm := &fakeVideoLLM{video: fewer}
	res, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(context.Background(), "https://www.instagram.com/reel/MORKOVKA1/")
	if err != nil {
		t.Fatal(err)
	}
	mustBeValidDraft(t, res.Draft)
	d := res.Draft
	if res.Report.Parser != ParserVideo || llm.videoCalls != 1 {
		t.Fatalf("report %+v, video calls %d", res.Report, llm.videoCalls)
	}
	// The caption's 9 quantified foods beat the video's 3; the caption's
	// title stays; the steps come from the video.
	if len(d.Ingredients) != 9 || d.Ingredients[0].Name != "Морковь" || d.Title != "Пикантная морковка по-корейски за 15 минут" || !strings.HasPrefix(d.Body, "1. Натереть свеклу.") {
		t.Errorf("draft = %+v", d)
	}

	// A video with more foods than the caption replaces the list.
	more := videoRecipe(t)
	for i := range 7 {
		more.Ingredients = append(more.Ingredients, domain.Ingredient{Name: "Специя " + string(rune('А'+i))})
	}
	res, err = Importer{Fetcher: f, LLM: &fakeVideoLLM{video: more}, Videos: NewVideoQuota(5)}.FromURL(context.Background(), "https://www.instagram.com/reel/MORKOVKA1/")
	if err != nil || len(res.Draft.Ingredients) != 10 || res.Draft.Ingredients[0].Name != "Свекла" {
		t.Errorf("ingredients = %+v, %v", res.Draft.Ingredients, err)
	}

	// The video holds no recipe: the caption's list stays, without error.
	res, err = Importer{Fetcher: f, LLM: &fakeVideoLLM{}, Videos: NewVideoQuota(5)}.FromURL(context.Background(), "https://www.instagram.com/reel/MORKOVKA1/")
	if err != nil || res.Report.Parser != ParserRules || len(res.Draft.Ingredients) != 9 {
		t.Errorf("empty video: %+v, %v", res.Report, err)
	}
}

func TestFromURLSkipsTheVideoOfAFullCaption(t *testing.T) {
	ig, f := newInstagram(t)
	llm := &fakeVideoLLM{video: videoRecipe(t)}
	res, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(context.Background(), "https://www.instagram.com/reel/KEKSY0001/")
	if err != nil || res.Report.Parser != ParserRules || llm.videoCalls != 0 || ig.embeds != 0 {
		t.Errorf("report %+v, %v; video calls %d, embed pages %d", res.Report, err, llm.videoCalls, ig.embeds)
	}
}

func TestFromURLVideoFallbacks(t *testing.T) {
	captionRecipe := Parsed{
		Ingredients: []domain.Ingredient{{Name: "Свекла", Quantity: mustQty(t, "1", "шт")}, {Name: "Капуста"}},
		Steps:       []string{"Сварить."},
		Confidence:  0.85,
	}
	ctx := context.Background()

	t.Run("photo post", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}, video: videoRecipe(t)}
		im := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}
		res, err := im.FromURL(ctx, "https://www.instagram.com/p/PHOTO0001/")
		if err != nil || res.Report.Parser != ParserLLM || llm.videoCalls != 0 || llm.calls != 1 || len(res.Report.Warnings) != 0 {
			t.Errorf("report %+v, %v; video calls %d, caption calls %d", res.Report, err, llm.videoCalls, llm.calls)
		}
		if im.Videos.used != 0 {
			t.Errorf("a post without video used the quota: %d", im.Videos.used)
		}
	})

	t.Run("video read fails", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}, videoErr: ErrLLMUnavailable}
		res, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/")
		if err != nil || res.Report.Parser != ParserLLM || llm.calls != 1 || !slices.Contains(res.Report.Warnings, warnVideoParse) {
			t.Errorf("report %+v, %v", res.Report, err)
		}
	})

	t.Run("video download fails", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}, video: videoRecipe(t)}
		im := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}
		res, err := im.FromURL(ctx, "https://www.instagram.com/reel/FORBIDDEN/")
		if err != nil || res.Report.Parser != ParserLLM || llm.videoCalls != 0 || !slices.Contains(res.Report.Warnings, warnVideoFetch) || im.Videos.used != 0 {
			t.Errorf("report %+v, %v; quota used %d", res.Report, err, im.Videos.used)
		}
	})

	t.Run("video holds no recipe", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}}
		_, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/")
		if !errors.Is(err, ErrNotARecipe) || errors.Is(err, ErrRecipeInVideo) || llm.calls != 0 {
			t.Errorf("err = %v, caption calls %d", err, llm.calls)
		}
	})

	t.Run("daily quota", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}, video: videoRecipe(t)}
		im := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(1)}
		if res, err := im.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/"); err != nil || res.Report.Parser != ParserVideo {
			t.Fatalf("first: %+v, %v", res.Report, err)
		}
		res, err := im.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT02/")
		if err != nil || res.Report.Parser != ParserLLM || llm.videoCalls != 1 || !slices.Contains(res.Report.Warnings, warnVideoQuota) {
			t.Errorf("second: %+v, %v; video calls %d", res.Report, err, llm.videoCalls)
		}
	})

	t.Run("model quota", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{err: errLLMQuota}, videoErr: fmt.Errorf("%w: generateContent: status 429", errLLMQuota)}
		res, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/")
		if err != nil || res.Report.Parser != ParserRules || !slices.Contains(res.Report.Warnings, warnModelQuota) || !strings.Contains(res.Draft.Body, sourceHeading) {
			t.Errorf("report %+v, %v", res.Report, err)
		}
	})

	t.Run("over the budget", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}, video: videoRecipe(t), videoDelay: 5 * time.Second}
		im := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5), videoBudget: 150 * time.Millisecond}
		start := time.Now()
		res, err := im.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/")
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("took %v", took)
		}
		if err != nil || res.Report.Parser != ParserLLM || !slices.Contains(res.Report.Warnings, warnVideoParse) {
			t.Errorf("report %+v, %v", res.Report, err)
		}
	})

	t.Run("pasted text never reads a video", func(t *testing.T) {
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: captionRecipe}, video: videoRecipe(t)}
		res, err := Importer{LLM: llm, Videos: NewVideoQuota(5)}.FromText(ctx, lowCaption)
		if err != nil || res.Report.Parser != ParserLLM || llm.videoCalls != 0 {
			t.Errorf("report %+v, %v; video calls %d", res.Report, err, llm.videoCalls)
		}
	})
}

func TestFromURLWithoutVideoParser(t *testing.T) {
	_, f := newInstagram(t)
	ctx := context.Background()
	for name, llm := range map[string]LLM{"no LLM": nil, "caption-only LLM": &fakeLLM{}} {
		_, err := Importer{Fetcher: f, LLM: llm}.FromURL(ctx, "https://www.instagram.com/reel/NOTRECIPE/")
		if !errors.Is(err, ErrRecipeInVideo) || !errors.Is(err, ErrNotARecipe) || !strings.Contains(err.Error(), "LLM_PROVIDER=gemini") {
			t.Errorf("%s: reel err = %v, want ErrRecipeInVideo", name, err)
		}
	}
	if _, err := (Importer{Fetcher: f}).FromURL(ctx, "https://www.instagram.com/p/NOTRECIPE/"); !errors.Is(err, ErrNotARecipe) || errors.Is(err, ErrRecipeInVideo) {
		t.Errorf("photo post err = %v, want a plain ErrNotARecipe", err)
	}
	if _, err := (Importer{}).FromText(ctx, readFixture(t, "34-negative-igra-gryaz")); errors.Is(err, ErrRecipeInVideo) {
		t.Errorf("pasted text err = %v", err)
	}

	// A caption list without steps: imported, with a hint about the video.
	res, err := Importer{Fetcher: f}.FromURL(ctx, "https://www.instagram.com/reel/MORKOVKA1/")
	if err != nil || len(res.Draft.Ingredients) != 9 || !slices.Contains(res.Report.Warnings, warnStepsInVideo) {
		t.Errorf("report %+v, %v", res.Report, err)
	}
}

// Diagnostics tell the operator why the model or the video failed, from a
// fixed vocabulary: never a key, a URL or a provider's message.
func TestReportDiagnostics(t *testing.T) {
	const secret = "sk-live-SECRET-42"
	ctx := context.Background()
	noLeak := func(t *testing.T, diags []string) {
		t.Helper()
		for _, d := range diags {
			for _, bad := range []string{secret, "http", "example", "token=", "10.0.0.1", "message", "valid", "Квота"} {
				if strings.Contains(d, bad) {
					t.Errorf("diagnostic %q holds %q", d, bad)
				}
			}
		}
	}

	t.Run("caption model", func(t *testing.T) {
		for want, err := range map[string]error{
			"llm: timeout": fmt.Errorf("%w: Post %q: dial tcp 10.0.0.1:443: i/o timeout", ErrLLMUnavailable, "https://api.example.com/v1/chat?token="+secret),
			"llm: status 401 UNAUTHENTICATED": statusError("generateContent", http.StatusUnauthorized,
				[]byte(`{"error":{"code":401,"message":"API key `+secret+` not valid","status":"UNAUTHENTICATED"}}`)),
			"llm: status 503":                  fmt.Errorf("%w: status 503", ErrLLMUnavailable),
			"llm: status 429, quota exhausted": fmt.Errorf("%w: generateContent: status 429", errLLMQuota),
			"llm: stopped: max_tokens":         fmt.Errorf("%w: stopped with %q", ErrLLMUnavailable, "max_tokens"),
			"llm: malformed answer":            fmt.Errorf("%w: not the expected JSON: invalid character near %s", ErrLLMUnavailable, secret),
			"llm: unavailable":                 errors.New("something odd about " + secret + " at https://example.com"),
		} {
			res, err := Importer{LLM: &fakeLLM{err: err}}.FromText(ctx, lowCaption)
			if err != nil || !slices.Equal(res.Report.Diagnostics, []string{want}) || !slices.Contains(res.Report.Warnings, warnLLMUnavailable) {
				t.Errorf("want %q: report %+v, %v", want, res.Report, err)
			}
			noLeak(t, res.Report.Diagnostics)
		}
	})

	t.Run("video model quota", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{err: errLLMQuota}, videoErr: statusError("generateContent", http.StatusTooManyRequests,
			[]byte(`{"error":{"code":429,"message":"Quota exceeded for `+secret+`","status":"RESOURCE_EXHAUSTED"}}`))}
		res, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/")
		want := []string{"video model: status 429 RESOURCE_EXHAUSTED, quota exhausted", "llm: quota exhausted"}
		if err != nil || !slices.Equal(res.Report.Diagnostics, want) {
			t.Errorf("diagnostics %q, %v; want %q", res.Report.Diagnostics, err, want)
		}
		noLeak(t, res.Report.Diagnostics)
	})

	t.Run("video download", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: videoRecipe(t)}, video: videoRecipe(t)}
		res, err := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(5)}.FromURL(ctx, "https://www.instagram.com/reel/FORBIDDEN/")
		if err != nil || !slices.Equal(res.Report.Diagnostics, []string{"video download: status 403"}) {
			t.Errorf("diagnostics %q, %v", res.Report.Diagnostics, err)
		}
		noLeak(t, res.Report.Diagnostics)
	})

	t.Run("daily video limit, then success", func(t *testing.T) {
		_, f := newInstagram(t)
		llm := &fakeVideoLLM{fakeLLM: fakeLLM{parsed: videoRecipe(t)}, video: videoRecipe(t)}
		im := Importer{Fetcher: f, LLM: llm, Videos: NewVideoQuota(1)}
		if res, err := im.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT01/"); err != nil || res.Report.Diagnostics != nil {
			t.Errorf("a clean import has diagnostics %q, %v", res.Report.Diagnostics, err)
		}
		res, err := im.FromURL(ctx, "https://www.instagram.com/reel/LOWCAPT02/")
		if err != nil || !slices.Equal(res.Report.Diagnostics, []string{"video: daily limit reached"}) {
			t.Errorf("diagnostics %q, %v", res.Report.Diagnostics, err)
		}
	})

	t.Run("a failed import keeps them in its error", func(t *testing.T) {
		llm := &fakeLLM{err: statusError("generateContent", http.StatusForbidden, []byte(`{"error":{"status":"PERMISSION_DENIED","message":"`+secret+`"}}`))}
		_, err := Importer{LLM: llm}.FromText(ctx, "Привет всем! Сегодня гуляли в парке")
		if !errors.Is(err, ErrNotARecipe) || !strings.Contains(err.Error(), "[llm: status 403 PERMISSION_DENIED]") || strings.Contains(err.Error(), secret) {
			t.Errorf("err = %v", err)
		}
		_, f := newInstagram(t)
		llm = &fakeLLM{err: fmt.Errorf("%w: status 401", ErrLLMUnavailable)}
		_, err = Importer{Fetcher: f, LLM: llm}.FromURL(ctx, "https://www.instagram.com/reel/NOTRECIPE/")
		if !errors.Is(err, ErrRecipeInVideo) || !strings.Contains(err.Error(), "[llm: status 401]") {
			t.Errorf("reel err = %v", err)
		}
	})

	t.Run("reel without a video reader", func(t *testing.T) {
		_, f := newInstagram(t)
		res, err := Importer{Fetcher: f}.FromURL(ctx, "https://www.instagram.com/reel/MORKOVKA1/")
		if err != nil || !slices.Equal(res.Report.Diagnostics, []string{OperatorHintVideo}) {
			t.Errorf("diagnostics %q, %v", res.Report.Diagnostics, err)
		}
	})
}

// What the user reads names nothing they cannot act on; the operator's
// hint goes to the log.
func TestUserTextNamesNoSetting(t *testing.T) {
	for _, text := range []string{
		HintRecipeInVideo, warnVideoQuota, warnVideoFetch, warnVideoParse, warnModelQuota, warnStepsInVideo, warnLLMUnavailable,
	} {
		if strings.ContainsAny(text, "=_") || strings.Contains(text, "LLM") || strings.Contains(text, "ключ") {
			t.Errorf("user text names a setting: %q", text)
		}
	}
	if !strings.Contains(OperatorHintVideo, "LLM_PROVIDER=gemini") || !strings.Contains(ErrRecipeInVideo.Error(), OperatorHintVideo) {
		t.Errorf("the operator hint %q must name the setting and be the error's text (%q)", OperatorHintVideo, ErrRecipeInVideo)
	}
}

func TestDiagnoseNeverCopiesTheError(t *testing.T) {
	for want, err := range map[string]error{
		"video download: status 403":               fmt.Errorf("%w: video status 403", ErrUnavailable),
		"video download: video too large":          fmt.Errorf("%w: video over 100 bytes", ErrUnavailable),
		"video download: no video URL":             fmt.Errorf("%w: the embed page gives no video URL", ErrUnavailable),
		"video download: timeout":                  fmt.Errorf("%w: downloading the video: %w", ErrUnavailable, context.DeadlineExceeded),
		"video model: timeout (video processing)":  fmt.Errorf("%w: the video was not processed in time", ErrLLMUnavailable),
		"video model: video processing failed":     fmt.Errorf("%w: Gemini could not process the video", ErrLLMUnavailable),
		"video model: blocked: PROHIBITED_CONTENT": fmt.Errorf("%w: no answer (blocked: PROHIBITED_CONTENT)", ErrLLMUnavailable),
		"video model: network error":               fmt.Errorf("%w: read tcp 192.0.2.1:1->192.0.2.2:443: connection reset by peer", ErrLLMUnavailable),
	} {
		step, _, _ := strings.Cut(want, ":")
		if got := diagnose(step, err); got != want {
			t.Errorf("diagnose(%q) = %q, want %q", err, got, want)
		}
	}
}
