// Package feedback sends problem reports to the Nahida backend: the text, the
// files the user attached and, when they agreed to it, the anonymized log.
package feedback

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// The limits mirror the backend's, so a report it would refuse is refused
// before anything is uploaded.
const (
	MaxBodyLength = 10000
	MaxMediaBytes = 100 << 20
	MaxMediaFiles = 20
	MaxModFiles   = 2000

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

// Kind is what the renderer shows an attachment as.
type Kind string

const (
	KindImage       Kind = "image"
	KindVideo       Kind = "video"
	KindText        Kind = "text"
	KindMod         Kind = "mod"
	KindUnsupported Kind = "unsupported"
)

var mediaTypes = map[string]Kind{
	".png": KindImage, ".jpg": KindImage, ".jpeg": KindImage, ".webp": KindImage,
	".gif": KindImage, ".avif": KindImage, ".bmp": KindImage,
	".mp4": KindVideo, ".webm": KindVideo, ".mov": KindVideo, ".mkv": KindVideo,
	".txt": KindText, ".log": KindText, ".md": KindText, ".json": KindText, ".ini": KindText,
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
	ModDirs []string `json:"modDirs"`
	Locale  string   `json:"locale"`
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

type Options struct {
	Remote    *infra.Client
	Log       *infra.Log
	EventEmit func(name string, data ...any)
	// LogPath locates desktop.log. It is resolved per report because the app
	// data root is opened after the services are built.
	LogPath func() (string, error)
	// ModFileRule fetches the upload rules mod attachments are held to.
	ModFileRule func(context.Context) (ModFileRule, error)
}

type Service struct {
	remote      *infra.Client
	log         *infra.Log
	emit        func(name string, data ...any)
	logPath     func() (string, error)
	modFileRule func(context.Context) (ModFileRule, error)

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
	}
}

// Describe classifies the paths the user picked or dropped, so the renderer can
// list them with their sizes before anything is sent.
func (s *Service) Describe(ctx context.Context, paths []string) ([]Attachment, error) {
	attachments := make([]Attachment, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		attachment := Attachment{Path: path, Name: filepath.Base(path), Size: info.Size()}
		if !info.IsDir() {
			attachment.Kind = mediaKind(path)
			attachments = append(attachments, attachment)
			continue
		}

		files, err := walkMod(ctx, path)
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
	if len(utf16.Encode([]rune(request.Body))) > MaxBodyLength {
		return Result{}, coded(codeBodyTooLong, "")
	}

	*stage = "collect"
	uploads, skipped, err := s.collect(ctx, request)
	if err != nil {
		return Result{}, err
	}
	reportID, err := newReportID()
	if err != nil {
		return Result{}, err
	}

	*stage = "upload"
	if err := s.upload(ctx, reportID, uploads); err != nil {
		return Result{}, err
	}

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
		Attachments: declarations(uploads),
	}, log)
	if err != nil {
		return Result{}, err
	}
	return Result{ID: id, SkippedModFiles: skipped}, nil
}

// upload is one file waiting for its presigned PUT.
type upload struct {
	declaration fileDeclaration
	source      string
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
func (s *Service) collect(ctx context.Context, request Request) ([]upload, int, error) {
	uploads := make([]upload, 0, len(request.Files))
	var mediaBytes int64
	for _, path := range request.Files {
		info, err := os.Stat(path)
		if err != nil {
			return nil, 0, err
		}
		if info.IsDir() || mediaKind(path) == KindUnsupported || info.Size() == 0 {
			return nil, 0, coded(codeUnsupportedFile, filepath.Base(path))
		}
		mediaBytes += info.Size()
		uploads = append(uploads, upload{
			source: path,
			declaration: fileDeclaration{
				Ref: fmt.Sprintf("m%d", len(uploads)), Kind: "media", Name: filepath.Base(path), Size: info.Size(),
			},
		})
	}
	if len(uploads) > MaxMediaFiles {
		return nil, 0, coded(codeTooManyMedia, "")
	}
	if mediaBytes > MaxMediaBytes {
		return nil, 0, coded(codeMediaTooLarge, "")
	}
	if len(request.ModDirs) == 0 {
		return uploads, 0, nil
	}

	if s.modFileRule == nil {
		return nil, 0, errors.New("the upload rules are not configured")
	}
	permitted, err := s.modFileRule(ctx)
	if err != nil {
		return nil, 0, err
	}
	skipped, modFiles := 0, 0
	for _, dir := range request.ModDirs {
		files, err := walkMod(ctx, dir)
		if err != nil {
			return nil, 0, err
		}
		for _, file := range files {
			// An empty file cannot be signed for: the declared size is part of
			// the upload URL and has to be positive.
			if file.size == 0 || !permitted(filepath.Base(file.path), file.size) {
				skipped++
				continue
			}
			uploads = append(uploads, upload{
				source: file.path,
				declaration: fileDeclaration{
					Ref: fmt.Sprintf("f%d", modFiles), Kind: "mod", Path: file.relative, Size: file.size,
				},
			})
			modFiles++
		}
	}
	if modFiles == 0 {
		return nil, 0, coded(codeEmptyMod, "")
	}
	if modFiles > MaxModFiles {
		return nil, 0, coded(codeTooManyModFiles, "")
	}
	return uploads, skipped, nil
}

type modFile struct {
	path string
	// relative is the slash-separated path below the mod folder's parent, so it
	// starts with the folder's own name.
	relative string
	size     int64
}

func walkMod(ctx context.Context, dir string) ([]modFile, error) {
	release, err := diskio.AcquireDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer release()

	root := filepath.Base(filepath.Clean(dir))
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

func mediaKind(path string) Kind {
	if kind, ok := mediaTypes[strings.ToLower(filepath.Ext(path))]; ok {
		return kind
	}
	return KindUnsupported
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
