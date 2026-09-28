package github

import (
	"context"
	"errors"
	"net/http"

	"nahida.live/desktop/internal/infra"
)

const fileUserAgent = "nahida-desktop"

// FileRequest downloads a release file or tag archive of Repo to Destination.
type FileRequest struct {
	Repo        Repo
	URL         string
	Destination string
	Progress    func(downloaded, total int64)
}

// DownloadFile streams a file to disk through the shared download policy.
func (c *Client) DownloadFile(ctx context.Context, request FileRequest) error {
	if err := request.Repo.Validate(); err != nil {
		return err
	}
	if c == nil || c.download == nil {
		return errors.New("GitHub download client is not configured")
	}
	download := infra.DownloadRequest{
		URL:         request.URL,
		Destination: request.Destination,
		Header:      fileHeader(request.Repo),
	}
	if request.Progress != nil {
		var downloaded, total int64
		download.OnResponse = func(length int64) {
			downloaded, total = 0, length
			request.Progress(downloaded, total)
		}
		download.Progress = func(bytes int64) {
			downloaded += bytes
			request.Progress(downloaded, total)
		}
	}
	return c.download.File(ctx, download)
}

// FetchFile reads a release file or raw repository file of at most limit bytes into memory.
func (c *Client) FetchFile(ctx context.Context, repo Repo, rawURL string, limit int64) ([]byte, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.http == nil {
		return nil, errHTTPNotConfigured
	}
	response, err := c.http.Fetch(
		ctx,
		rawURL,
		infra.FetchOptions{Method: http.MethodGet, Header: fileHeader(repo), DisableHTTPErrors: true},
	)
	if err != nil {
		return nil, err
	}
	if response.Body == nil {
		return nil, errors.New("empty GitHub file response")
	}
	defer func() { _ = response.Body.Close() }()
	return readResponse(response, rawURL, limit)
}

func fileHeader(repo Repo) http.Header {
	header := make(http.Header)
	header.Set("User-Agent", fileUserAgent)
	header.Set("Referer", repo.webURL())
	return header
}
