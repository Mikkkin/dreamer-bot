// Package recipeimport turns an Instagram post (or a pasted caption) into
// a recipe draft: title, ingredients with quantities, numbered steps,
// servings and the cover picture.
//
// A post is read the way a link preview reads it: one GET of the canonical
// post URL with the link-preview user agent, whose og:description carries
// the full caption. No login, cookie or API is used. The caption is parsed
// by deterministic rules (Parse); when they are unsure and an LLM is
// configured, the caption goes to the model as untrusted data and its
// answer is validated like the rules' output. Many reels keep the recipe
// in the video only: when the LLM can also read videos (VideoParser) and
// the caption holds no usable recipe or no steps, the video is downloaded
// through the post's embed page and Instagram's CDN and read by the model.
// Captions and videos are never stored here; the importer only returns
// the draft.
package recipeimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

var (
	// ErrBadURL means the text holds no Instagram post link.
	ErrBadURL = errors.New("recipeimport: not an Instagram post link")
	// ErrUnavailable means Instagram did not give the post: a network
	// error, a timeout, a status other than 200, a redirect elsewhere, an
	// oversized page or no caption. The user can paste the caption
	// instead. It matches domain.ErrExternalUnavailable.
	ErrUnavailable = fmt.Errorf("recipeimport: Instagram did not return the post: %w", domain.ErrExternalUnavailable)
	// ErrNoCaption is the ErrUnavailable of a page without og:description
	// (a login wall, a removed or private post).
	ErrNoCaption = fmt.Errorf("%w (no caption on the page)", ErrUnavailable)
	// ErrNotARecipe means the text holds no recipe: no ingredients and no
	// steps, or too little to trust.
	ErrNotARecipe = errors.New("recipeimport: the text is not a recipe")
	// ErrRecipeInVideo is the ErrNotARecipe of a reel whose caption holds
	// no recipe while no LLM that reads videos is configured: the recipe
	// is probably only in the video. HintRecipeInVideo tells the user;
	// the error's own text carries OperatorHintVideo for the log.
	ErrRecipeInVideo = fmt.Errorf("%w: %s", ErrNotARecipe, OperatorHintVideo)
)

// HintRecipeInVideo is the user's message for ErrRecipeInVideo. It names
// nothing the user cannot act on: no key, no setting.
const HintRecipeInVideo = "Рецепт, похоже, только в видео — вставьте текст рецепта или подписи"

// OperatorHintVideo tells the operator how to read reels whose recipe is
// only in the video. It is for logs and the README, never for users; it
// is the text of ErrRecipeInVideo and a Report.Diagnostics entry when a
// reel's steps are missing.
const OperatorHintVideo = "the recipe is probably only in the video: set LLM_PROVIDER=gemini and LLM_API_KEY to read reels"

// Report sources and parsers.
const (
	SourceInstagram = "instagram"
	SourceText      = "text"
	ParserRules     = "rules"
	ParserLLM       = "llm"
	ParserVideo     = "video"
)

// DefaultThreshold is the rules' confidence below which the LLM (when
// configured) parses the caption instead.
const DefaultThreshold = 0.6

// VideoBudget bounds the whole video path of one import: the download,
// the upload to the model, its processing and the answer.
const VideoBudget = 90 * time.Second

// Fallback titles for captions that never name the dish.
const (
	FallbackTitleInstagram = "Рецепт из Instagram"
	FallbackTitleText      = "Рецепт"
)

// Warnings of the video path.
const (
	warnVideoQuota     = "Лимит разбора видео на сегодня исчерпан — разобрана только подпись"
	warnVideoFetch     = "Видео не удалось скачать — разобрана только подпись"
	warnVideoParse     = "Видео разобрать не удалось — разобрана только подпись"
	warnModelQuota     = "Квота Gemini исчерпана — разобрана только подпись"
	warnStepsInVideo   = "Шаги, похоже, только в видео — допишите их в рецепт"
	warnLLMUnavailable = "Умный разбор недоступен — текст разобран по правилам"
)

// sourceHeading precedes the original caption appended to the body of a
// low-confidence import, so nothing the parser missed is lost.
const sourceHeading = "Исходный текст:"

// Importer builds recipe drafts from Instagram links and pasted captions.
// A nil Fetcher uses a shared default one; a nil LLM disables the model
// fallback; Threshold 0 means DefaultThreshold. Videos bounds the videos
// sent to the model a day; nil shares one process-wide quota of
// DefaultVideoDailyLimit.
type Importer struct {
	Fetcher   *Fetcher
	LLM       LLM
	Threshold float64
	Videos    *VideoQuota

	videoBudget time.Duration // 0 means VideoBudget; tests shorten it
}

// Result is a draft ready for the normal recipe validation, the cover
// picture (nil when there is none) and a report for the user.
type Result struct {
	Draft  domain.RecipeDraft
	Image  []byte
	Report Report
}

// Report tells how the draft was made: Source is "instagram" or "text",
// Parser "rules", "llm" (the caption read by the model) or "video" (the
// video read by the model, with the caption as context), Confidence the
// parser's score (0..1), Image whether a cover picture was downloaded,
// Warnings short Russian notes for the user.
//
// Diagnostics are for the operator's log, never for the user: why the
// model or the video step failed («llm: status 401», «video model: status
// 429 RESOURCE_EXHAUSTED, quota exhausted», «video download: timeout»), or
// OperatorHintVideo. They are built from a fixed vocabulary, status codes
// and Google's status names only, so they never hold a key, a URL, a
// caption or a response body. Nil when nothing failed. When the import
// fails all the same (ErrNotARecipe after a failed model call), the
// error's text carries them in brackets.
type Report struct {
	Source      string
	Parser      string
	Confidence  float64
	Image       bool
	Warnings    []string
	Diagnostics []string
}

var sharedFetcher = sync.OnceValue(NewFetcher)

// postVideo is the video of the post being imported, fetched only when
// the importer decides to read it.
type postVideo struct {
	fetcher *Fetcher
	ref     Ref
	reel    bool
}

// FromURL imports the post behind an Instagram link: ErrBadURL when raw
// holds no post link, ErrUnavailable when Instagram does not give the
// post, ErrNotARecipe when neither its caption nor (with a VideoParser)
// its video holds a recipe (ErrRecipeInVideo for a reel read without
// one). The draft's link is the canonical post URL. A cover picture or a
// video that fails to download is a warning, not an error. The video
// path may take up to VideoBudget.
func (im Importer) FromURL(ctx context.Context, raw string) (Result, error) {
	ref, ok := ParseURL(raw)
	if !ok {
		return Result{}, ErrBadURL
	}
	f := im.Fetcher
	if f == nil {
		f = sharedFetcher()
	}
	post, err := f.Post(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	res, err := im.build(ctx, post.Caption, SourceInstagram, FallbackTitleInstagram, &postVideo{fetcher: f, ref: ref, reel: post.Reel})
	if err != nil {
		return Result{}, err
	}
	link := ref.URL()
	res.Draft.Link = &link
	if post.ImageURL != "" {
		img, err := f.Image(ctx, post.ImageURL)
		if err != nil {
			res.Report.Warnings = append(res.Report.Warnings, "Обложку не удалось загрузить")
		} else {
			res.Image, res.Report.Image = img, true
		}
	}
	return res, nil
}

// FromText imports a pasted caption or recipe text: ErrNotARecipe when it
// is not a recipe.
func (im Importer) FromText(ctx context.Context, text string) (Result, error) {
	return im.build(ctx, text, SourceText, FallbackTitleText, nil)
}

// build parses the caption with the rules, then lets the model read the
// video (video != nil, a VideoParser configured, and the caption unsure
// or without steps) or else the caption (rules unsure, an LLM
// configured).
func (im Importer) build(ctx context.Context, caption, source, fallbackTitle string, video *postVideo) (Result, error) {
	caption = strings.TrimSpace(caption)
	threshold := im.Threshold
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	vp, readsVideo := im.LLM.(VideoParser)
	notARecipe := ErrNotARecipe
	if video != nil && video.reel && !readsVideo {
		notARecipe = ErrRecipeInVideo
	}
	p := Parse(caption)
	parser := ParserRules
	var notes, diags []string
	if video != nil && readsVideo && (p.Confidence < threshold || len(p.Steps) == 0) {
		vr, ok, note, diag := im.readVideo(ctx, vp, video, caption)
		if note != "" {
			notes = append(notes, note)
		}
		if diag != "" {
			diags = append(diags, diag)
		}
		switch {
		case !ok:
		case len(vr.Ingredients) == 0 && len(vr.Steps) == 0:
			if p.Confidence < threshold {
				// The model saw the video and the caption and found no
				// recipe.
				return Result{}, withDiagnostics(ErrNotARecipe, diags)
			}
		default:
			p, parser = mergeVideo(p, vr, threshold), ParserVideo
		}
	}
	if parser == ParserRules && p.Confidence < threshold && im.LLM != nil && caption != "" {
		lp, err := im.LLM.Parse(ctx, caption)
		switch {
		case err != nil:
			notes = append(notes, warnLLMUnavailable)
			diags = append(diags, diagnose("llm", err))
		case len(lp.Ingredients) == 0 && len(lp.Steps) == 0:
			// The model, which reads meaning better than the rules, found
			// no recipe either.
			return Result{}, withDiagnostics(notARecipe, diags)
		default:
			if lp.Title == "" {
				lp.Title = p.Title
			}
			if lp.Servings == 0 {
				lp.Servings = p.Servings
			}
			p, parser = lp, ParserLLM
		}
	}
	if parser == ParserRules && p.notARecipe() {
		return Result{}, withDiagnostics(notARecipe, diags)
	}
	if video != nil && video.reel && !readsVideo && len(p.Steps) == 0 {
		notes = append(notes, warnStepsInVideo)
		diags = append(diags, OperatorHintVideo)
	}
	title := p.Title
	if title == "" {
		title = fallbackTitle
	}
	body := numberedSteps(p.Steps)
	if parser == ParserRules && p.Confidence < threshold {
		body = withSource(body, caption)
		notes = append(notes, "Разбор неуверенный — исходный текст сохранён в описании рецепта")
	}
	draft := domain.RecipeDraft{
		Title:       title,
		Body:        body,
		Ingredients: p.Ingredients,
	}
	if p.Servings >= 1 && p.Servings <= domain.MaxServings {
		s := p.Servings
		draft.Servings = &s
	}
	return Result{
		Draft: draft,
		Report: Report{
			Source:      source,
			Parser:      parser,
			Confidence:  math.Round(p.Confidence*100) / 100,
			Warnings:    append(p.Warnings, notes...),
			Diagnostics: diags,
		},
	}, nil
}

// readVideo downloads the post's video and has the model read it, within
// the video budget, the daily quota and the slots for videos in memory.
// ok is false when the post has no video (note "") or the video path
// failed (note says so for the user, diag why for the operator).
func (im Importer) readVideo(ctx context.Context, vp VideoParser, video *postVideo, caption string) (p Parsed, ok bool, note, diag string) {
	quota := im.Videos
	if quota == nil {
		quota = sharedVideoQuota()
	}
	if !quota.take() {
		if !video.reel {
			return Parsed{}, false, "", "" // perhaps not a video at all
		}
		return Parsed{}, false, warnVideoQuota, "video: daily limit reached"
	}
	budget := im.videoBudget
	if budget <= 0 {
		budget = VideoBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	select {
	case videoSlots <- struct{}{}:
		defer func() { <-videoSlots }()
	case <-ctx.Done():
		quota.refund()
		return Parsed{}, false, warnVideoParse, "video: timeout waiting for a free slot"
	}
	data, mimeType, err := video.fetcher.Video(ctx, video.ref)
	if err != nil {
		// Nothing reached the model: the quota is not spent.
		quota.refund()
		if errors.Is(err, ErrNoVideo) {
			return Parsed{}, false, "", ""
		}
		return Parsed{}, false, warnVideoFetch, diagnose("video download", err)
	}
	p, err = vp.ParseVideo(ctx, data, mimeType, caption)
	switch {
	case errors.Is(err, errLLMQuota):
		return Parsed{}, false, warnModelQuota, diagnose("video model", err)
	case err != nil:
		return Parsed{}, false, warnVideoParse, diagnose("video model", err)
	}
	return p, true, "", ""
}

// withDiagnostics adds the diagnostics of a failed import to its error's
// text for the operator's log; errors.Is still sees err.
func withDiagnostics(err error, diags []string) error {
	if len(diags) == 0 {
		return err
	}
	return fmt.Errorf("%w [%s]", err, strings.Join(diags, "; "))
}

var (
	diagStatusRe = regexp.MustCompile(`status (\d{3})(?: ([A-Z][A-Z_]{0,39}))?`)
	diagStopRe   = regexp.MustCompile(`stopped with "?([A-Za-z_]{1,40})`)
	diagBlockRe  = regexp.MustCompile(`blocked: ([A-Za-z_]{1,40})`)
)

// diagCauses map a fragment of an error's text to the operator's name for
// it, first match first.
var diagCauses = []struct{ fragment, cause string }{
	{"deadline exceeded", "timeout"},
	{"Client.Timeout", "timeout"},
	{"i/o timeout", "timeout"},
	{"not processed in time", "timeout (video processing)"},
	{"context canceled", "canceled"},
	{"could not process the video", "video processing failed"},
	{"malformed", "malformed answer"},
	{"not the expected JSON", "malformed answer"},
	{"data after the JSON", "malformed answer"},
	{"response over", "answer too large"},
	{"video over", "video too large"},
	{"empty video", "not a video"},
	{"not a video", "not a video"},
	{"not a usable video", "not a usable video"},
	{"video host not allowed", "video host not allowed"},
	{"gives no video URL", "no video URL"},
	{"unreadable video URL", "no video URL"},
	{"no usable", "unexpected API answer"},
	{"refusing a request off", "unexpected API answer"},
	{"connection refused", "network error"},
	{"connection reset", "network error"},
	{"no such host", "network error"},
	{"EOF", "network error"},
}

// diagnose names why a model or video step failed, for the operator's
// log: the step, the provider's status (with Google's status name), a
// quota, a timeout or the kind of failure. The error's text is never
// copied: it may hold a URL with signed tokens or a provider's message.
func diagnose(step string, err error) string {
	msg := err.Error()
	var causes []string
	if m := diagStatusRe.FindStringSubmatch(msg); m != nil {
		causes = append(causes, strings.TrimSpace("status "+m[1]+" "+m[2]))
	}
	if errors.Is(err, errLLMQuota) || strings.Contains(msg, "RESOURCE_EXHAUSTED") {
		causes = append(causes, "quota exhausted")
	}
	if m := diagStopRe.FindStringSubmatch(msg); m != nil {
		causes = append(causes, "stopped: "+m[1])
	}
	if m := diagBlockRe.FindStringSubmatch(msg); m != nil {
		causes = append(causes, "blocked: "+m[1])
	}
	if errors.Is(err, context.DeadlineExceeded) {
		causes = append(causes, "timeout")
	}
	for _, c := range diagCauses {
		if strings.Contains(msg, c.fragment) {
			if !slices.Contains(causes, c.cause) {
				causes = append(causes, c.cause)
			}
			break
		}
	}
	if len(causes) == 0 {
		causes = append(causes, "unavailable")
	}
	return step + ": " + strings.Join(causes, ", ")
}

// mergeVideo takes the model's reading of the video. A caption the rules
// read with confidence keeps its title, servings and, when the video
// gives fewer foods, its ingredient list: the caption is what the author
// wrote down, and the video mostly adds the steps.
func mergeVideo(rules, video Parsed, threshold float64) Parsed {
	out := video
	confident := rules.Confidence >= threshold
	if confident && len(rules.Ingredients) > len(video.Ingredients) {
		out.Ingredients, out.Warnings = rules.Ingredients, rules.Warnings
	}
	if (confident && rules.Title != "") || out.Title == "" {
		out.Title = rules.Title
	}
	if (confident && rules.Servings != 0) || out.Servings == 0 {
		out.Servings = rules.Servings
	}
	if len(out.Steps) == 0 {
		out.Steps = rules.Steps
	}
	out.Confidence = modelConfidence(len(out.Ingredients), len(out.Steps))
	return out
}

// numberedSteps renders steps as «1. …\n2. …» within the body limit.
func numberedSteps(steps []string) string {
	var b strings.Builder
	runes := 0
	for i, s := range steps {
		line := strconv.Itoa(i+1) + ". " + s
		n := utf8.RuneCountInString(line)
		if i > 0 {
			n++ // the newline
		}
		if runes+n > domain.MaxRecipeBodyLen {
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		runes += n
	}
	return b.String()
}

// withSource appends the original text under sourceHeading, cut to fit the
// body limit.
func withSource(body, caption string) string {
	prefix := sourceHeading + "\n"
	if body != "" {
		prefix = body + "\n\n" + prefix
	}
	room := domain.MaxRecipeBodyLen - utf8.RuneCountInString(prefix)
	if room <= 1 {
		return body
	}
	if utf8.RuneCountInString(caption) > room {
		caption = string([]rune(caption)[:room-1]) + "…"
	}
	return prefix + caption
}
