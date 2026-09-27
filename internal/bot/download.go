package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	tg "github.com/go-telegram/bot"
)

// maxBotDownload is the Bot API limit for getFile downloads.
const maxBotDownload int64 = 20 << 20

const downloadTimeout = 60 * time.Second

var errFileTooLarge = errors.New("bot: file too large")

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// downloader fetches files users sent to the bot. Download URLs contain the
// bot token, so every error leaving this type is redacted.
type downloader struct {
	files  fileSource
	client httpDoer
	max    int64
	redact redactor
}

func newDownloader(files fileSource, maxImageBytes int64, r redactor) *downloader {
	return &downloader{
		files:  files,
		client: newDownloadClient(),
		max:    min(maxBotDownload, maxImageBytes),
		redact: r,
	}
}

// newDownloadClient refuses redirects: following one would send the
// token-bearing URL to another host in the Referer header.
func newDownloadClient() *http.Client {
	return &http.Client{
		Timeout: downloadTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// fetch downloads the file with the given ID, reading at most max bytes.
func (d *downloader) fetch(ctx context.Context, fileID string) ([]byte, error) {
	data, err := d.fetchRaw(ctx, fileID)
	if err != nil {
		return nil, d.redact.err(err)
	}
	return data, nil
}

func (d *downloader) fetchRaw(ctx context.Context, fileID string) ([]byte, error) {
	f, err := d.files.GetFile(ctx, &tg.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("bot: getFile: %w", err)
	}
	if f.FileSize > d.max {
		return nil, errFileTooLarge
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.files.FileDownloadLink(f), nil)
	if err != nil {
		return nil, fmt.Errorf("bot: download request: %w", err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bot: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bot: download: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, d.max+1))
	if err != nil {
		return nil, fmt.Errorf("bot: download body: %w", err)
	}
	if int64(len(data)) > d.max {
		return nil, errFileTooLarge
	}
	return data, nil
}
