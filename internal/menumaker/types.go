package menumaker

type MenuMakerScanStats struct {
	Directories int `json:"directories"`
	Files       int `json:"files"`
	INI         int `json:"ini"`
	TXT         int `json:"txt"`
	Listed      int `json:"listed"`
	Disabled    int `json:"disabled"`
	Errors      int `json:"errors"`
}

type MenuMakerScanFile struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	Kind         string `json:"kind"`
}

type MenuMakerScanResult struct {
	RootPath string              `json:"rootPath"`
	Files    []MenuMakerScanFile `json:"files"`
	Stats    MenuMakerScanStats  `json:"stats"`
}

type MenuMakerSource struct {
	Path     string            `json:"path"`
	FileName string            `json:"fileName"`
	Text     string            `json:"text"`
	SHA256   string            `json:"sha256"`
	Encoding string            `json:"encoding"`
	HasBOM   bool              `json:"hasBOM"`
	Newline  string            `json:"newline"`
	Document MenuMakerDocument `json:"document"`
}

type MenuMakerGeneratedAsset struct {
	RelativePath string `json:"relativePath"`
	Data         []byte `json:"data"`
}

type MenuMakerApplyRequest struct {
	SourcePath         string                    `json:"sourcePath"`
	SourceSHA256       string                    `json:"sourceSHA256"`
	OutputININame      string                    `json:"outputININame"`
	Slots              []MenuMakerSlot           `json:"slots"`
	Settings           MenuMakerSettings         `json:"settings"`
	Encoding           string                    `json:"encoding"`
	HasBOM             bool                      `json:"hasBOM"`
	Newline            string                    `json:"newline"`
	Assets             []MenuMakerGeneratedAsset `json:"assets"`
	UseOriginalININame bool                      `json:"useOriginalININame"`
}

type MenuMakerSaveINIRequest struct {
	DestinationPath string            `json:"destinationPath"`
	SourceText      string            `json:"sourceText"`
	Slots           []MenuMakerSlot   `json:"slots"`
	Settings        MenuMakerSettings `json:"settings"`
	Encoding        string            `json:"encoding"`
	HasBOM          bool              `json:"hasBOM"`
	Newline         string            `json:"newline"`
}

type MenuMakerSaveZIPRequest struct {
	DestinationPath string                    `json:"destinationPath"`
	OutputININame   string                    `json:"outputININame"`
	SourceText      string                    `json:"sourceText"`
	Slots           []MenuMakerSlot           `json:"slots"`
	Settings        MenuMakerSettings         `json:"settings"`
	Encoding        string                    `json:"encoding"`
	HasBOM          bool                      `json:"hasBOM"`
	Newline         string                    `json:"newline"`
	Assets          []MenuMakerGeneratedAsset `json:"assets"`
}

type MenuMakerWriteResult struct {
	OutputINIPath string   `json:"outputINIPath,omitempty"`
	ArchivePath   string   `json:"archivePath,omitempty"`
	BackupPath    string   `json:"backupPath,omitempty"`
	SourceSHA256  string   `json:"sourceSHA256,omitempty"`
	ResourcePaths []string `json:"resourcePaths"`
	RolledBack    bool     `json:"rolledBack"`
}

type MenuMakerEntry struct {
	Kind     string   `json:"kind"`
	Variable string   `json:"variable,omitempty"`
	Values   []string `json:"values,omitempty"`
	Target   string   `json:"target,omitempty"`
	Raw      string   `json:"raw,omitempty"`
	Line     string   `json:"line,omitempty"`
}

type MenuMakerSection struct {
	Name  *string  `json:"name"`
	Lines []string `json:"lines"`
	Index int      `json:"index"`
}

type MenuMakerHandler struct {
	ID                  string           `json:"id"`
	Section             string           `json:"section"`
	SourceIndex         int              `json:"sourceIndex"`
	Keys                []string         `json:"keys"`
	Key                 string           `json:"key"`
	Condition           string           `json:"condition"`
	Type                string           `json:"type"`
	Back                string           `json:"back"`
	Wrap                bool             `json:"wrap"`
	Entries             []MenuMakerEntry `json:"entries"`
	Assignments         []MenuMakerEntry `json:"assignments"`
	CommandLists        []string         `json:"commandLists"`
	RawEntries          []string         `json:"rawEntries"`
	Steps               int              `json:"steps"`
	CommandName         string           `json:"commandName"`
	BackCommandName     string           `json:"backCommandName"`
	ActivateCommandName string           `json:"activateCommandName"`
	StepVar             string           `json:"stepVar"`
	ActivatePulseVar    string           `json:"activatePulseVar"`
}

type MenuMakerSlot struct {
	ID           string             `json:"id"`
	Key          string             `json:"key"`
	OriginalKeys []string           `json:"originalKeys"`
	Handlers     []MenuMakerHandler `json:"handlers"`
	Name         string             `json:"name"`
	Skip         bool               `json:"skip"`
	MergeMode    string             `json:"mergeMode,omitempty"`
}

type MenuMakerDocument struct {
	Text     string             `json:"text"`
	Sections []MenuMakerSection `json:"sections"`
	Handlers []MenuMakerHandler `json:"handlers"`
	Slots    []MenuMakerSlot    `json:"slots"`
}

type MenuMakerPalette struct {
	Accent               string `json:"accent"`
	PanelBackground      string `json:"panelBackground"`
	PanelBackgroundAlpha int    `json:"panelBackgroundAlpha"`
	PanelBorder          string `json:"panelBorder"`
	PanelBorderAlpha     int    `json:"panelBorderAlpha"`
	SlotBackground       string `json:"slotBackground"`
	SlotBackgroundAlpha  int    `json:"slotBackgroundAlpha"`
	SlotHover            string `json:"slotHover"`
	SlotHoverAlpha       int    `json:"slotHoverAlpha"`
	SlotBorder           string `json:"slotBorder"`
	SlotBorderAlpha      int    `json:"slotBorderAlpha"`
	Title                string `json:"title"`
	TitleShadow          string `json:"titleShadow"`
}

type MenuMakerSettings struct {
	Title                string           `json:"title"`
	MenuKey              string           `json:"menuKey"`
	ClickModifier        string           `json:"clickModifier"`
	Columns              int              `json:"columns"`
	Gap                  int              `json:"gap"`
	BaseWidth            int              `json:"baseWidth"`
	BaseHeight           int              `json:"baseHeight"`
	PanelScale           float64          `json:"panelScale"`
	SlotAlignment        string           `json:"slotAlignment"`
	FallbackType         string           `json:"fallbackType"`
	RemoveOriginalKeys   bool             `json:"removeOriginalKeys"`
	ShowKeyHint          bool             `json:"showKeyHint"`
	HideUploadLabel      bool             `json:"hideUploadLabel"`
	UseOriginalININame   bool             `json:"useOriginalININame"`
	ResetActiveOnPresent bool             `json:"resetActiveOnPresent"`
	Palette              MenuMakerPalette `json:"palette"`
}

type MenuMakerSlotPosition struct {
	SlotID     string `json:"slotId"`
	AssetIndex int    `json:"assetIndex"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Size       int    `json:"size"`
}

type MenuMakerGeometry struct {
	PanelWidth  int                     `json:"panelWidth"`
	PanelHeight int                     `json:"panelHeight"`
	SlotSize    int                     `json:"slotSize"`
	Padding     int                     `json:"padding"`
	TitleHeight int                     `json:"titleHeight"`
	ScaledGap   int                     `json:"scaledGap"`
	Slots       []MenuMakerSlotPosition `json:"slots"`
}

type MenuMakerSlotValueState struct {
	Variable       string `json:"variable"`
	Value          string `json:"value"`
	Active         bool   `json:"active"`
	ResourceSuffix string `json:"resourceSuffix"`
	FileSuffix     string `json:"fileSuffix"`
}

type MenuMakerSlotStateGroup struct {
	SlotID string                    `json:"slotId"`
	States []MenuMakerSlotValueState `json:"states"`
}

type MenuMakerGenerateRequest struct {
	SourceText string            `json:"sourceText"`
	Slots      []MenuMakerSlot   `json:"slots"`
	Settings   MenuMakerSettings `json:"settings"`
}

type MenuMakerGenerateResult struct {
	INIText    string                    `json:"iniText"`
	Geometry   MenuMakerGeometry         `json:"geometry"`
	SlotStates []MenuMakerSlotStateGroup `json:"slotStates"`
	AssetPaths []string                  `json:"assetPaths"`
}
