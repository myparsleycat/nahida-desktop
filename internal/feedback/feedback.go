// Package feedback sends problem reports to the Nahida backend: the text, the
// files the user attached and, when they agreed to it, the anonymized log.
package feedback

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	// maxLogBytes is how much of the end of desktop.log a report carries.
	maxLogBytes = 8 << 20

	// EventProgress carries the upload progress of the report being sent.
	EventProgress = "feedback:progress"
)

// The error codes the renderer translates. Anything else is shown as a generic
// failure with the backend's own message.
const (
	codeInvalidCategory = "FEEDBACK_INVALID_CATEGORY"
	codeEmptyBody       = "FEEDBACK_EMPTY_BODY"
	codeBodyTooLong     = "FEEDBACK_BODY_TOO_LONG"
	codeUnsupportedFile = "FEEDBACK_UNSUPPORTED_FILE"
	codeMediaTooLarge   = "FEEDBACK_MEDIA_TOO_LARGE"
	codeTooManyMedia    = "FEEDBACK_TOO_MANY_MEDIA"
	codeTooManyModFiles = "FEEDBACK_TOO_MANY_MOD_FILES"
	codeEmptyMod        = "FEEDBACK_EMPTY_MOD"
	codeBusy            = "FEEDBACK_BUSY"
)

func coded(code, detail string) error {
	return infra.ContractError(code + ":" + detail)
}

// Kind is what the renderer shows an attachment as. The media kinds are the
// server's, so one it adds later reaches the renderer unchanged.
type Kind string

const (
	KindImage       Kind = "image"
	KindVideo       Kind = "video"
	KindText        Kind = "text"
	KindMod         Kind = "mod"
	KindUnsupported Kind = "unsupported"
)

// Rules are the server's feedback upload rules: the media allow list and the
// bounds of one report, checked before anything is uploaded. Mod files follow
// the akasha upload rules instead.
type Rules struct {
	MediaExtensions []MediaExtension `json:"mediaExtensions"`
	MaxBodyLength   int              `json:"maxBodyLength"`
	MaxMediaFiles   int              `json:"maxMediaFiles"`
	MaxMediaBytes   int64            `json:"maxMediaBytes"`
	MaxModFiles     int              `json:"maxModFiles"`
	MaxPlanFiles    int              `json:"maxPlanFiles"`
}

// MediaExtension is one extension the server accepts as a media attachment.
type MediaExtension struct {
	Ext  string `json:"ext"`
	Kind Kind   `json:"kind"`
}

func (r Rules) usable() bool {
	if len(r.MediaExtensions) == 0 || r.MaxBodyLength <= 0 || r.MaxMediaFiles <= 0 || r.MaxMediaBytes <= 0 ||
		r.MaxModFiles <= 0 || r.MaxPlanFiles <= 0 {
		return false
	}
	for _, media := range r.MediaExtensions {
		if !strings.HasPrefix(media.Ext, ".") || media.Kind == "" || media.Kind == KindMod ||
			media.Kind == KindUnsupported {
			return false
		}
	}
	return true
}

func (r Rules) mediaKind(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	for _, media := range r.MediaExtensions {
		if media.Ext == ext {
			return media.Kind
		}
	}
	return KindUnsupported
}

var categories = map[string]bool{"service": true, "desktop": true, "xxmi-libs": true}

// Attachment describes one path the user picked: a media file, or a folder
// that is attached as a mod.
type Attachment struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	Size int64  `json:"size"`
	// Files is how many files a mod folder holds.
	Files int `json:"files"`
}

// Request is one report as the renderer submits it.
type Request struct {
	Category   string `json:"category"`
	Body       string `json:"body"`
	IncludeLog bool   `json:"includeLog"`
	// Files are the media files and ModDirs the mod folders to attach.
	Files   []string `json:"files"`
	ModDirs []ModDir `json:"modDirs"`
	Locale  string   `json:"locale"`
}

// ModDir is a mod folder to attach. Its files are declared below Name, a slash-separated path that keeps folders
// picked from different parents apart, or below the folder's own name when Name is empty.
type ModDir struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// Result reports what a submitted report stored.
type Result struct {
	ID string `json:"id"`
	// SkippedModFiles counts the mod files the upload rules do not admit.
	SkippedModFiles int `json:"skippedModFiles"`
}

// Progress is the payload of EventProgress.
type Progress struct {
	UploadedBytes int64 `json:"uploadedBytes"`
	TotalBytes    int64 `json:"totalBytes"`
}

// ModFileRule answers whether the server's upload rules admit a mod file.
type ModFileRule func(name string, size int64) bool

// ModUploader sends the mod files of a report through the akasha upload
// intents.
type ModUploader interface {
	UploadFeedbackMod(
		ctx context.Context,
		reportID string,
		files []drive.FeedbackModFile,
		onProgress func(bytes int64),
	) (drive.FeedbackModResult, error)
}

type Options struct {
	Remote    *infra.Client
	Log       *infra.Log
	EventEmit func(name string, data ...any)
	// LogPath locates desktop.log. It is resolved per report because the app
	// data root is opened after the services are built.
	LogPath func() (string, error)
	// ModFileRule fetches the upload rules mod attachments are held to.
	ModFileRule func(context.Context) (ModFileRule, error)
	Mods        ModUploader
}

type Service struct {
	remote      *infra.Client
	log         *infra.Log
	emit        func(name string, data ...any)
	logPath     func() (string, error)
	modFileRule func(context.Context) (ModFileRule, error)
	mods        ModUploader

	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(options Options) *Service {
	return &Service{
		remote:      options.Remote,
		log:         options.Log,
		emit:        options.EventEmit,
		logPath:     options.LogPath,
		modFileRule: options.ModFileRule,
		mods:        options.Mods,
	}
}

// Rules fetches the server's feedback upload rules, which the renderer checks a
// report against before sending it.
func (s *Service) Rules(ctx context.Context) (Rules, error) {
	if s.remote == nil {
		return Rules{}, errors.New("the Nahida backend is not configured")
	}
	data, err := s.backend(ctx, http.MethodGet, rulesPath, nil, nil)
	if err != nil {
		return Rules{}, err
	}
	var rules Rules
	if err := json.Unmarshal(data, &rules); err != nil {
		return Rules{}, fmt.Errorf("decode the feedback upload rules: %w", err)
	}
	if !rules.usable() {
		return Rules{}, errors.New("the server sent unusable feedback upload rules")
	}
	return rules, nil
}

// Describe classifies the paths the user picked or dropped, so the renderer can
// list them with their sizes before anything is sent.
func (s *Service) Describe(ctx context.Context, paths []string) ([]Attachment, error) {
	attachments := make([]Attachment, 0, len(paths))
	var rules *Rules
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		attachment := Attachment{Path: path, Name: filepath.Base(path), Size: info.Size()}
		if !info.IsDir() {
			if rules == nil {
				fetched, err := s.Rules(ctx)
				if err != nil {
					return nil, err
				}
				rules = &fetched
			}
			attachment.Kind = rules.mediaKind(path)
			attachments = append(attachments, attachment)
			continue
		}

		files, err := walkMod(ctx, path, attachment.Name)
		if err != nil {
			return nil, err
		}
		attachment.Kind, attachment.Size, attachment.Files = KindMod, 0, len(files)
		for _, file := range files {
			attachment.Size += file.size
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

// Cancel stops the report being sent, if there is one.
func (s *Service) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Submit uploads the attachments and stores the report.
func (s *Service) Submit(ctx context.Context, request Request) (Result, error) {
	if s.remote == nil {
		return Result{}, errors.New("the Nahida backend is not configured")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !s.begin(cancel) {
		return Result{}, coded(codeBusy, "a report is already being sent")
	}
	defer s.end()

	stage := "validate"
	result, err := s.submit(ctx, request, &stage)
	if err == nil || infra.IsCancellationError(err) {
		return result, err
	}
	return Result{}, infra.ReportError(s.log, err, "Feedback", infra.Diagnostic{
		Operation: "submit-feedback",
		Stage:     stage,
		Fields: map[string]any{
			"category":   request.Category,
			"includeLog": request.IncludeLog,
			"files":      request.Files,
			"modDirs":    request.ModDirs,
		},
	})
}

func (s *Service) begin(cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return false
	}
	s.cancel = cancel
	return true
}

func (s *Service) end() {
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
}

func (s *Service) submit(ctx context.Context, request Request, stage *string) (Result, error) {
	if !categories[request.Category] {
		return Result{}, coded(codeInvalidCategory, request.Category)
	}
	if strings.TrimSpace(request.Body) == "" {
		return Result{}, coded(codeEmptyBody, "")
	}

	*stage = "rules"
	rules, err := s.Rules(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(utf16.Encode([]rune(request.Body))) > rules.MaxBodyLength {
		return Result{}, coded(codeBodyTooLong, "")
	}

	*stage = "collect"
	media, mods, skipped, err := s.collect(ctx, request, rules)
	if err != nil {
		return Result{}, err
	}
	reportID, err := newReportID()
	if err != nil {
		return Result{}, err
	}
	progress := s.newProgress(media, mods)
	progress.report(true)

	*stage = "upload"
	if err := s.upload(ctx, reportID, media, rules.MaxPlanFiles, progress); err != nil {
		return Result{}, err
	}

	*stage = "upload-mod"
	modDeclarations, refused, err := s.uploadMods(ctx, reportID, mods, progress)
	if err != nil {
		return Result{}, err
	}
	progress.report(true)

	*stage = "log"
	var log []byte
	if request.IncludeLog {
		log = s.readLog()
	}

	*stage = "submit"
	id, err := s.send(ctx, reportPayload{
		ReportID:    reportID,
		Category:    request.Category,
		Source:      "desktop",
		Body:        request.Body,
		AppVersion:  platform.AppVersion,
		Platform:    "windows",
		Locale:      request.Locale,
		Environment: environment(),
		Attachments: append(declarations(media), modDeclarations...),
	}, log)
	if err != nil {
		return Result{}, err
	}
	return Result{ID: id, SkippedModFiles: skipped + refused}, nil
}

// upload is one media file waiting for its presigned PUT.
type upload struct {
	declaration fileDeclaration
	source      string
}

// modUpload is one mod file the upload rules admit, waiting for the akasha
// upload intents.
type modUpload struct {
	ref    string
	source modFile
}

// uploadMods sends the mod files through the akasha upload intents, the way a
// drive upload sends files, and answers their declarations and how many the
// server refused by its rules. Those are left out like the files the rules
// refused before the upload.
func (s *Service) uploadMods(
	ctx context.Context,
	reportID string,
	mods []modUpload,
	progress *progress,
) ([]fileDeclaration, int, error) {
	if len(mods) == 0 {
		return nil, 0, nil
	}
	if s.mods == nil {
		return nil, 0, errors.New("the mod uploader is not configured")
	}

	files := make([]drive.UploadFile, len(mods))
	for index, mod := range mods {
		files[index] = drive.UploadFile{
			FID: mod.ref, Name: filepath.Base(mod.source.path), Size: mod.source.size, FullPath: mod.source.path,
		}
	}
	var modBytes, hashedBytes int64
	for _, mod := range mods {
		modBytes += mod.source.size
	}
	hashed, err := drive.HashUploadFiles(ctx, files, max(2, runtime.NumCPU()/2), func(completed int) {
		credited := modBytes * int64(completed) / int64(len(files))
		progress.add(credited - hashedBytes)
		hashedBytes = credited
	})
	if err != nil {
		return nil, 0, err
	}
	planned := make([]drive.FeedbackModFile, len(mods))
	for index, mod := range mods {
		planned[index] = drive.FeedbackModFile{
			Ref:      mod.ref,
			Path:     mod.source.relative,
			FullPath: mod.source.path,
			Size:     mod.source.size,
			SHA256:   hashed[index].SHA256,
		}
	}

	result, err := s.mods.UploadFeedbackMod(ctx, reportID, planned, progress.add)
	if err != nil {
		return nil, 0, err
	}
	stored := make(map[string]struct{}, len(result.Stored))
	for _, ref := range result.Stored {
		stored[ref] = struct{}{}
	}
	out := make([]fileDeclaration, 0, len(result.Stored))
	for _, file := range planned {
		if _, ok := stored[file.Ref]; ok {
			out = append(out, fileDeclaration{
				Ref: file.Ref, Kind: "mod", Path: file.Path, Size: file.Size, SHA256: file.SHA256,
			})
		}
	}
	if len(out) == 0 {
		return nil, 0, coded(codeEmptyMod, "")
	}
	return out, result.Refused, nil
}

func declarations(uploads []upload) []fileDeclaration {
	out := make([]fileDeclaration, 0, len(uploads))
	for _, item := range uploads {
		out = append(out, item.declaration)
	}
	return out
}

// collect turns the picked paths into the files to upload. Mod files the
// upload rules do not admit are left out and counted, the way a drive upload
// leaves them out, instead of failing the whole report.
func (s *Service) collect(ctx context.Context, request Request, rules Rules) ([]upload, []modUpload, int, error) {
	uploads := make([]upload, 0, len(request.Files))
	var mediaBytes int64
	for _, path := range request.Files {
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, 0, err
		}
		if info.IsDir() || rules.mediaKind(path) == KindUnsupported || info.Size() == 0 {
			return nil, nil, 0, coded(codeUnsupportedFile, filepath.Base(path))
		}
		mediaBytes += info.Size()
		uploads = append(uploads, upload{
			source: path,
			declaration: fileDeclaration{
				Ref: fmt.Sprintf("m%d", len(uploads)), Kind: "media", Name: filepath.Base(path), Size: info.Size(),
			},
		})
	}
	if len(uploads) > rules.MaxMediaFiles {
		return nil, nil, 0, coded(codeTooManyMedia, "")
	}
	if mediaBytes > rules.MaxMediaBytes {
		return nil, nil, 0, coded(codeMediaTooLarge, "")
	}
	if len(request.ModDirs) == 0 {
		return uploads, nil, 0, nil
	}

	if s.modFileRule == nil {
		return nil, nil, 0, errors.New("the upload rules are not configured")
	}
	permitted, err := s.modFileRule(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	skipped := 0
	var mods []modUpload
	for _, dir := range request.ModDirs {
		root := filepath.Base(filepath.Clean(dir.Path))
		if dir.Name != "" {
			name := filepath.FromSlash(dir.Name)
			if !filepath.IsLocal(name) {
				return nil, nil, 0, fmt.Errorf("mod folder name %q is not a relative path", dir.Name)
			}
			root = filepath.ToSlash(filepath.Clean(name))
		}
		files, err := walkMod(ctx, dir.Path, root)
		if err != nil {
			return nil, nil, 0, err
		}
		for _, file := range files {
			// The server keys content by a positive size, so an empty file has
			// nothing to upload.
			if file.size == 0 || !permitted(filepath.Base(file.path), file.size) {
				skipped++
				continue
			}
			mods = append(mods, modUpload{ref: fmt.Sprintf("f%d", len(mods)), source: file})
		}
	}
	if len(mods) == 0 {
		return nil, nil, 0, coded(codeEmptyMod, "")
	}
	if len(mods) > rules.MaxModFiles {
		return nil, nil, 0, coded(codeTooManyModFiles, "")
	}
	return uploads, mods, skipped, nil
}

type modFile struct {
	path string
	// relative is the slash-separated path the file is declared under, which
	// starts with the root its mod folder is attached as.
	relative string
	size     int64
}

func walkMod(ctx context.Context, dir, root string) ([]modFile, error) {
	release, err := diskio.AcquireDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer release()

	var files []modFile
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, modFile{
			path:     path,
			relative: root + "/" + filepath.ToSlash(relative),
			size:     info.Size(),
		})
		return nil
	})
	return files, err
}

// readLog returns the anonymized end of desktop.log, or nil when there is no
// log to attach. A report is worth sending without it, so a failure here is
// reported and does not stop the report.
func (s *Service) readLog() []byte {
	if s.logPath == nil {
		return nil
	}
	path, err := s.logPath()
	if err == nil {
		var data []byte
		if data, err = readTail(path, maxLogBytes); err == nil {
			return []byte(infra.AnonymizeLogText(string(data)))
		}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		_ = infra.ReportError(s.log, err, "Feedback", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "submit-feedback", Stage: "read-log",
		})
	}
	return nil
}

// readTail reads the last limit bytes of a file, starting at a line boundary so
// the first record is not cut in half.
func readTail(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(0, info.Size()-limit)
	data := make([]byte, info.Size()-offset)
	read, err := file.ReadAt(data, offset)
	if err != nil && read != len(data) {
		return nil, err
	}
	if offset == 0 {
		return data, nil
	}
	if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
		return data[newline+1:], nil
	}
	return data, nil
}

func environment() map[string]string {
	version := windows.RtlGetVersion()
	return map[string]string{
		"os":   fmt.Sprintf("Windows %d.%d.%d", version.MajorVersion, version.MinorVersion, version.BuildNumber),
		"arch": runtime.GOARCH,
	}
}

func newReportID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
