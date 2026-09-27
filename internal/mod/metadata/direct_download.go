package metadata

const DirectSource = "mod"

// DirectDownload is the nhd.json schema for downloads from a URL.
type DirectDownload struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	DownloadedAt string `json:"downloadedAt"`
}
