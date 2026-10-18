package efmioutline

type OutlineFileState string

const (
	StateOriginal    OutlineFileState = "original"
	StateApplied     OutlineFileState = "applied"
	StateModified    OutlineFileState = "modified"
	StateUnsupported OutlineFileState = "unsupported"
)

type OutlineInspection struct {
	ModPath string        `json:"modPath"`
	Files   []OutlineFile `json:"files"`
}

type OutlineFile struct {
	// Path is relative to the mod folder.
	Path      string              `json:"path"`
	State     OutlineFileState    `json:"state"`
	HasBackup bool                `json:"hasBackup"`
	Parts     []OutlinePart       `json:"parts"`
	Manual    []OutlineManualItem `json:"manual"`
}

type OutlinePart struct {
	ID       string   `json:"id"`
	Section  string   `json:"section"`
	Label    string   `json:"label"`
	Sections []string `json:"sections"`
	Lines    []int    `json:"lines"`
	Applied  bool     `json:"applied"`
	// Locked marks a part the original file already keeps out of the outline
	// pass; restoring the original cannot bring it back.
	Locked bool `json:"locked"`
}

type OutlineManualItem struct {
	Section string `json:"section"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Reason  string `json:"reason"`
}

type OutlineSelection struct {
	Path    string   `json:"path"`
	PartIDs []string `json:"partIds"`
}

type OutlineApplyInput struct {
	ModPath string             `json:"modPath"`
	Files   []OutlineSelection `json:"files"`
}
