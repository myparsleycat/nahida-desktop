package metadata

const GameBananaSource = "gamebanana"

// GameBananaDownload is the nhd.json schema for a GameBanana download.
type GameBananaDownload struct {
	ID           string           `json:"id"`
	Source       string           `json:"source"`
	DownloadedAt string           `json:"downloadedAt"`
	Mod          *GameBananaMod   `json:"mod"`
	Author       GameBananaAuthor `json:"author"`
	File         GameBananaFile   `json:"file"`
}

type GameBananaMod struct {
	ID      int64   `json:"id"`
	PageURL string  `json:"pageUrl"`
	Version *string `json:"version"`
}

type GameBananaAuthor struct {
	Name *string `json:"name"`
	URL  *string `json:"url"`
}

type GameBananaFile struct {
	DownloadURL string  `json:"downloadUrl"`
	MD5         *string `json:"md5"`
}
