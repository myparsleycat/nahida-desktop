package drive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

func TestUploadPartsReportsWaitingAndResumesAfterDelayedResponse(t *testing.T) {
	const partSize = 128 * 1024
	firstReceived := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		if request.URL.Path == "/parts/0" {
			close(firstReceived)
			select {
			case <-release:
			case <-request.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(release) })
	path := writeUploadContent(t, "delayed.bin", bytes.Repeat([]byte("x"), 2*partSize))
	rules := testUploadRules()
	rules.Parts.MaxBytes = partSize
	var transferred atomic.Int64
	phases := make(chan transfer.UploadPhase, 16)
	activity := &uploadActivity{onProgress: func(progress UploadExecutionProgress) { phases <- progress.Phase }}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), uploadActivityKey{}, activity))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- uploadTestDrive(server).uploadParts(ctx, UploadPlanEntry{URL: server.URL}, FinalUploadFile{
			UploadFile: UploadFile{Name: "delayed.bin", FullPath: filepath.ToSlash(path), Size: 2 * partSize},
		}, rules, false, nil, func(bytes int64) { transferred.Add(bytes) })
	}()
	select {
	case <-firstReceived:
	case <-time.After(time.Second):
		t.Fatal("first upload part did not reach the server")
	}
	if transferred.Load() != partSize {
		t.Fatalf("bytes during response wait = %d, want %d", transferred.Load(), partSize)
	}
	for _, want := range []transfer.UploadPhase{transfer.UploadTransferring, transfer.UploadWaiting} {
		select {
		case got := <-phases:
			if got != want {
				t.Fatalf("upload phase = %s, want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing upload phase %s", want)
		}
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("upload did not resume after the response")
	}
	if transferred.Load() != 2*partSize {
		t.Fatalf("final uploaded bytes = %d", transferred.Load())
	}
	if got := <-phases; got != transfer.UploadTransferring {
		t.Fatalf("resumed upload phase = %s", got)
	}
}

type uploadRoundTripFunc func(*http.Request) (*http.Response, error)

func (f uploadRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func uploadTestDrive(server *httptest.Server) *Drive {
	drive := NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: server.Client(),
			Status:     infra.BackendOnline,
		}),
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	return withUploadRules(drive, testUploadRules())
}

func TestUploadSourceFailuresAreMarked(t *testing.T) {
	t.Parallel()
	file := FinalUploadFile{UploadFile: UploadFile{FullPath: filepath.Join(t.TempDir(), "gone.ini"), Name: "gone.ini"}}
	if _, _, err := prepareDirectUpload(file, UploadCompressionRules{}); !errors.Is(err, ErrBackupSourceRead) {
		t.Fatalf("direct source error = %v", err)
	}
	if err := (&Drive{}).uploadParts(
		t.Context(),
		UploadPlanEntry{},
		file,
		UploadRules{},
		false,
		nil,
		nil,
	); !errors.Is(
		err,
		ErrBackupSourceRead,
	) {
		t.Fatalf("multipart source error = %v", err)
	}
	if _, err := io.ReadAll(
		&backupSourceReader{reader: bytes.NewReader([]byte("x")), remaining: 2},
	); !errors.Is(
		err,
		ErrBackupSourceRead,
	) {
		t.Fatalf("short multipart source error = %v", err)
	}
}

func TestUploadIntentSendsDirectMultipart(t *testing.T) {
	content := []byte("small")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("token") != "token" {
			t.Fatalf("token = %q", request.FormValue("token"))
		}
		file, header, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		got, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if header.Filename != "file.ini" || string(got) != string(content) {
			t.Fatalf("filename = %q, content = %q", header.Filename, got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"completed"}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL}
	upload.Form.Token = "token"
	progress := int64(0)
	err := uploadTestDrive(server).uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, func(bytes int64) { progress += bytes })
	if err != nil {
		t.Fatal(err)
	}
	if progress != int64(len(content)) {
		t.Fatalf("progress = %d", progress)
	}
}

func TestUploadIntentCompressesNonPreviewDirectFile(t *testing.T) {
	content := make([]byte, 4096)
	for index := range content {
		content[index] = byte(index % 251)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("compAlg") != "zstd" {
			t.Fatalf("compAlg = %q", request.FormValue("compAlg"))
		}
		file, _, err := request.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		compressed, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			t.Fatal(err)
		}
		decompressed, err := decoder.DecodeAll(compressed, nil)
		decoder.Close()
		if err != nil || string(decompressed) != string(content) {
			t.Fatalf("decompressed upload differs: error = %v, bytes = %d", err, len(decompressed))
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "data.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL}
	upload.Form.Token = "token"
	if err := uploadTestDrive(server).uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "data.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestUploadIntentRollsBackFailedAttemptProgress(t *testing.T) {
	content := []byte("small payload")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL}
	upload.Form.Token = "token"
	progress := int64(0)
	if err := uploadTestDrive(server).uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, func(bytes int64) { progress += bytes }); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || progress != int64(len(content)) {
		t.Fatalf("requests = %d, progress = %d", requests.Load(), progress)
	}
}

func TestUploadIntentPollsAcceptedUploadWithoutResendingPayload(t *testing.T) {
	content := []byte("accepted payload")
	var uploadRequests atomic.Int32
	var statusRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/status") {
			if statusRequests.Add(1) == 1 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"status":"processing","nextAction":"poll","retryAfterMs":1}`)
				return
			}
			_, _ = io.WriteString(w, `{"status":"completed"}`)
			return
		}

		uploadRequests.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"processing","nextAction":"poll","retryAfterMs":1}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL + "/v2/uploads/intent"}
	upload.Form.Token = "token"
	if err := uploadTestDrive(server).uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if uploadRequests.Load() != 1 || statusRequests.Load() != 2 {
		t.Fatalf("upload requests = %d, status requests = %d", uploadRequests.Load(), statusRequests.Load())
	}
}

func TestUploadIntentDoesNotResendForMissingIntentStatus(t *testing.T) {
	content := []byte("accepted payload")
	var uploadRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/status") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "intent_not_found")
			return
		}

		uploadRequests.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"processing","nextAction":"poll"}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL + "/v2/uploads/missing"}
	upload.Form.Token = "token"
	err := uploadTestDrive(server).uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, nil)
	var uploadErr *UploadV2Error
	if !errors.As(err, &uploadErr) || uploadErr.Code != "intent_not_found" {
		t.Fatalf("error = %v, want intent_not_found", err)
	}
	if uploadRequests.Load() != 1 {
		t.Fatalf("upload requests = %d, want 1", uploadRequests.Load())
	}
}

func TestUploadIntentFallsBackWhenStatusEndpointIsUnavailable(t *testing.T) {
	content := []byte("accepted payload")
	var uploadRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/status") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "Not Found")
			return
		}

		_, _ = io.Copy(io.Discard, request.Body)
		w.Header().Set("Content-Type", "application/json")
		if uploadRequests.Add(1) == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"pending"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"completed"}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL + "/v2/uploads/intent"}
	upload.Form.Token = "token"
	if err := uploadTestDrive(server).uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if uploadRequests.Load() != 2 {
		t.Fatalf("upload requests = %d, want the legacy retry", uploadRequests.Load())
	}
}

func TestUploadIntentRetriesTransportError(t *testing.T) {
	content := []byte("retry me")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	var requests atomic.Int32
	base := server.Client().Transport
	client := &http.Client{Transport: uploadRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			return nil, errors.New("temporary transport failure")
		}
		return base.RoundTrip(request)
	})}
	drive := NewWithOptions(Options{
		HTTP:  infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: client, Status: infra.BackendOnline}),
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	withUploadRules(drive, testUploadRules())
	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL}
	upload.Form.Token = "token"
	progress := int64(0)
	if err := drive.uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, func(bytes int64) { progress += bytes }); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || progress != int64(len(content)) {
		t.Fatalf("requests = %d, progress = %d", requests.Load(), progress)
	}
}

func TestUploadIntentUsesPartsWhenDirectBodyExceedsLimit(t *testing.T) {
	content := []byte("small")
	var partRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/parts/0"):
			partRequests.Add(1)
			if err := request.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(request.URL.Path, "/complete"):
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	drive := uploadTestDrive(server)
	rules := testUploadRules()
	rules.MaxUploadBodyBytes = 32
	withUploadRules(drive, rules)
	upload := UploadPlanEntry{URL: server.URL}
	upload.Form.Token = "token"
	if err := drive.uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if partRequests.Load() != 1 {
		t.Fatalf("part requests = %d, want 1", partRequests.Load())
	}
}

func TestUploadPartsPollsAcceptedCompletionWithoutResendingParts(t *testing.T) {
	content := []byte("small")
	var partRequests atomic.Int32
	var completeRequests atomic.Int32
	var statusRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, "/parts/0"):
			partRequests.Add(1)
			_, _ = io.Copy(io.Discard, request.Body)
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(request.URL.Path, "/complete"):
			completeRequests.Add(1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"status":"processing","nextAction":"poll","retryAfterMs":1}`)
		case strings.HasSuffix(request.URL.Path, "/status"):
			statusRequests.Add(1)
			_, _ = io.WriteString(w, `{"status":"completed"}`)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "file.ini")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	drive := uploadTestDrive(server)
	rules := testUploadRules()
	rules.MaxUploadBodyBytes = 32
	withUploadRules(drive, rules)
	upload := UploadPlanEntry{URL: server.URL + "/v2/uploads/intent"}
	upload.Form.Token = "token"
	if err := drive.uploadIntent(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if partRequests.Load() != 1 || completeRequests.Load() != 1 || statusRequests.Load() != 1 {
		t.Fatalf(
			"part requests = %d, complete requests = %d, status requests = %d",
			partRequests.Load(),
			completeRequests.Load(),
			statusRequests.Load(),
		)
	}
}

func TestDirectUploadExceedsMaxBodyIncludesMultipartOverhead(t *testing.T) {
	upload := UploadPlanEntry{}
	upload.Form.Token = "token"
	file := FinalUploadFile{UploadFile: UploadFile{Name: "file.ini"}}
	data := []byte("small")
	if !directUploadExceedsMaxBody(file, data, "", upload, int64(len(data))) {
		t.Fatal("payload equal to max body should still exceed after multipart overhead")
	}
	if directUploadExceedsMaxBody(file, data, "", upload, 1024) {
		t.Fatal("small direct request should fit a 1KiB body limit")
	}
}

func TestPrepareDirectUploadFollowsTheCompressionRules(t *testing.T) {
	compression := testUploadRules().Compression
	content := bytes.Repeat([]byte("compressible payload"), 512)
	path := writeUploadContent(t, "data.ini", content)
	file := FinalUploadFile{
		UploadFile: UploadFile{Name: "data.ini", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}

	data, algorithm, err := prepareDirectUpload(file, compression)
	if err != nil {
		t.Fatal(err)
	}
	if algorithm != compression.Algorithm {
		t.Fatalf("algorithm = %q, want %q", algorithm, compression.Algorithm)
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(compression.Level)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = encoder.Close() }()
	if want := encoder.EncodeAll(content, nil); !bytes.Equal(data, want) {
		t.Fatalf("payload = %d bytes, want an encode at level %d", len(data), compression.Level)
	}
}

func TestPrepareDirectUploadSkipsWhatTheRulesSkip(t *testing.T) {
	compression := testUploadRules().Compression
	tests := []struct {
		name    string
		content []byte
	}{
		{
			name:    "file at the skip size",
			content: bytes.Repeat([]byte("x"), int(compression.SkipMaxBytes)),
		},
		{
			name: "image content under an unknown name",
			content: append(
				[]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
				bytes.Repeat([]byte{0x00}, 4096)...,
			),
		},
		{
			name: "already-compressed container",
			content: append(
				[]byte{'P', 'K', 0x03, 0x04, 0x14, 0x00, 0x00, 0x00, 0x08, 0x00},
				bytes.Repeat([]byte{0x00}, 4096)...,
			),
		},
		{
			name:    "gzip stream",
			content: append([]byte{0x1f, 0x8b, 0x08, 0x00}, bytes.Repeat([]byte{0x00}, 4096)...),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeUploadContent(t, "payload.bin", tc.content)
			file := FinalUploadFile{
				UploadFile: UploadFile{
					Name: "payload.bin", FullPath: filepath.ToSlash(path), Size: int64(len(tc.content)),
				},
			}

			data, algorithm, err := prepareDirectUpload(file, compression)
			if err != nil {
				t.Fatal(err)
			}
			if algorithm != "" || !bytes.Equal(data, tc.content) {
				t.Fatalf("algorithm = %q, payload = %d bytes", algorithm, len(data))
			}
		})
	}
}

func TestPrepareDirectUploadMatchesBareMimeTypes(t *testing.T) {
	compression := testUploadRules().Compression
	compression.SkipMimeTypes = []string{"text/plain"}
	content := bytes.Repeat([]byte("plain text payload "), 16)
	path := writeUploadContent(t, "notes.bin", content)
	file := FinalUploadFile{
		UploadFile: UploadFile{Name: "notes.bin", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}

	data, algorithm, err := prepareDirectUpload(file, compression)
	if err != nil {
		t.Fatal(err)
	}
	if algorithm != "" || !bytes.Equal(data, content) {
		t.Fatalf("algorithm = %q, payload = %d bytes", algorithm, len(data))
	}
}

func writeUploadContent(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUploadPartsResendsAfterMissingManifest(t *testing.T) {
	content := []byte("multipart payload")
	var partRequests atomic.Int32
	var completeRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/parts/0":
			partRequests.Add(1)
			if err := request.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{}`)
		case "/complete":
			if completeRequests.Add(1) == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"reason":"chunk_manifest_not_found"}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	upload := UploadPlanEntry{URL: server.URL}
	upload.Form.Token = "token"
	progress := int64(0)
	if err := uploadTestDrive(server).uploadParts(context.Background(), upload, FinalUploadFile{
		UploadFile: UploadFile{Name: "file.bin", FullPath: filepath.ToSlash(path), Size: int64(len(content))},
	}, testUploadRules(), false, nil, func(bytes int64) { progress += bytes }); err != nil {
		t.Fatal(err)
	}
	if partRequests.Load() != 2 || completeRequests.Load() != 2 || progress != int64(len(content)) {
		t.Fatalf(
			"part requests = %d, complete requests = %d, progress = %d",
			partRequests.Load(),
			completeRequests.Load(),
			progress,
		)
	}
}

const testUploadPartSize = 16 * 1024

type uploadPartsTest struct {
	parts   int
	slots   partSlots
	timeout time.Duration
	now     func() time.Time
}

// uploadTestParts sends a file of the given number of parts to the handler and
// answers the bytes reported once the upload returns.
func uploadTestParts(t *testing.T, test uploadPartsTest, handler http.HandlerFunc) (int64, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := *server.Client()
	client.Timeout = test.timeout
	drive := withUploadRules(NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: &client,
			Status:     infra.BackendOnline,
		}),
		Sleep: func(context.Context, time.Duration) error { return nil },
		Now:   test.now,
	}), testUploadRules())

	size := int64(test.parts) * testUploadPartSize
	path := writeUploadContent(t, "parts.bin", bytes.Repeat([]byte("x"), int(size)))
	rules := testUploadRules()
	rules.Parts.MaxBytes = testUploadPartSize
	var progress atomic.Int64
	err := drive.uploadParts(t.Context(), UploadPlanEntry{URL: server.URL}, FinalUploadFile{
		UploadFile: UploadFile{Name: "parts.bin", FullPath: filepath.ToSlash(path), Size: size},
	}, rules, false, test.slots, func(bytes int64) { progress.Add(bytes) })
	return progress.Load(), err
}

// steppedClock answers a time that moves on by step at every reading, so the
// time between two readings is the step whatever the speed of the runner.
func steppedClock(step time.Duration) func() time.Time {
	var mu sync.Mutex
	now := time.Unix(0, 0)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(step)
		return now
	}
}

func TestUploadPartsSendsFirstPartAloneThenInParallel(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		upload uploadPartsTest
		want   int32
	}{
		"file limit": {upload: uploadPartsTest{parts: 5}, want: maxMultipartUploadConcurrency},
		"run slots":  {upload: uploadPartsTest{parts: 5, slots: make(partSlots, 2)}, want: 2},
		"slow first part": {
			upload: uploadPartsTest{parts: 5, timeout: 100 * time.Second, now: steppedClock(20 * time.Second)},
			want:   2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var active, peak, completes atomic.Int32
			together := make(chan struct{})
			var togetherOnce sync.Once
			progress, err := uploadTestParts(t, test.upload, func(w http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				if request.URL.Path == "/complete" {
					completes.Add(1)
					_, _ = io.WriteString(w, `{}`)
					return
				}
				current := active.Add(1)
				for seen := peak.Load(); current > seen && !peak.CompareAndSwap(seen, current); seen = peak.Load() {
				}
				if request.URL.Path == "/parts/0" {
					if current != 1 {
						t.Errorf("first part shared the uplink with %d requests", current-1)
					}
				} else {
					if current == test.want {
						togetherOnce.Do(func() { close(together) })
					}
					select {
					case <-together:
					case <-time.After(5 * time.Second):
						t.Errorf("only %d parts were in flight together, want %d", peak.Load(), test.want)
					}
				}
				active.Add(-1)
				_, _ = io.WriteString(w, `{}`)
			})
			if err != nil {
				t.Fatal(err)
			}
			if peak.Load() != test.want || completes.Load() != 1 || progress != 5*testUploadPartSize {
				t.Fatalf("peak = %d, completes = %d, progress = %d", peak.Load(), completes.Load(), progress)
			}
		})
	}
}

func TestUploadPartsRollsBackProgressWhenPartIsRefused(t *testing.T) {
	t.Parallel()
	progress, err := uploadTestParts(
		t,
		uploadPartsTest{parts: 4},
		func(w http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			if request.URL.Path == "/parts/2" {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"reason":"chunk_owner_mismatch"}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		},
	)
	var uploadErr *UploadV2Error
	if !errors.As(err, &uploadErr) || uploadErr.Code != "chunk_owner_mismatch" {
		t.Fatalf("error = %v", err)
	}
	if progress != 0 {
		t.Fatalf("progress after failure = %d, want 0", progress)
	}
}

func TestUploadPartsStopsWhenServerAlreadyHoldsFile(t *testing.T) {
	t.Parallel()
	progress, err := uploadTestParts(
		t,
		uploadPartsTest{parts: 4},
		func(w http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			switch request.URL.Path {
			case "/parts/2":
				_, _ = io.WriteString(w, `{"status":"completed"}`)
			case "/complete":
				t.Error("completed upload was completed again")
			default:
				_, _ = io.WriteString(w, `{}`)
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if progress != 4*testUploadPartSize {
		t.Fatalf("progress = %d, want %d", progress, 4*testUploadPartSize)
	}
}

func TestUploadPartsRetriesPartAlone(t *testing.T) {
	t.Parallel()
	var active, retryPeers, firstTry atomic.Int32
	release := make(chan struct{})
	progress, err := uploadTestParts(
		t,
		uploadPartsTest{parts: 6},
		func(w http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			if request.URL.Path == "/complete" {
				_, _ = io.WriteString(w, `{}`)
				return
			}
			current := active.Add(1)
			switch request.URL.Path {
			case "/parts/1":
				if firstTry.Add(1) == 1 {
					active.Add(-1)
					w.WriteHeader(http.StatusInternalServerError)
					close(release)
					return
				}
				time.Sleep(20 * time.Millisecond)
				retryPeers.Store(max(current, active.Load()) - 1)
			case "/parts/2", "/parts/3", "/parts/4":
				select {
				case <-release:
				case <-request.Context().Done():
					return
				}
			}
			active.Add(-1)
			_, _ = io.WriteString(w, `{}`)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstTry.Load() != 2 || retryPeers.Load() != 0 || progress != 6*testUploadPartSize {
		t.Fatalf("tries = %d, retry peers = %d, progress = %d", firstTry.Load(), retryPeers.Load(), progress)
	}
}

func TestUploadPartsSendsOneAtATimeAfterTimeout(t *testing.T) {
	t.Parallel()
	var tries [8]atomic.Int32
	var active, peak atomic.Int32
	progress, err := uploadTestParts(
		t,
		// A clock that stands still times the first part at nothing, which
		// allows the full width whatever the speed of the runner.
		uploadPartsTest{parts: len(tries), timeout: time.Second, now: steppedClock(0)},
		func(w http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			index, err := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/parts/"))
			if err != nil {
				_, _ = io.WriteString(w, `{}`)
				return
			}
			if tries[index].Add(1) == 1 && index >= 1 && index <= maxMultipartUploadConcurrency {
				<-request.Context().Done()
				return
			}
			if index > 0 {
				current := active.Add(1)
				for seen := peak.Load(); current > seen && !peak.CompareAndSwap(seen, current); seen = peak.Load() {
				}
				time.Sleep(10 * time.Millisecond)
				active.Add(-1)
			}
			_, _ = io.WriteString(w, `{}`)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 1 || progress != int64(len(tries))*testUploadPartSize {
		t.Fatalf("peak after timeout = %d, progress = %d", peak.Load(), progress)
	}
}

func TestPartParallelismLeavesRoomInsideRequestTimeout(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		elapsed, timeout time.Duration
		want             int
	}{
		{elapsed: 0, timeout: 100 * time.Second, want: maxMultipartUploadConcurrency},
		{elapsed: time.Minute, timeout: 0, want: maxMultipartUploadConcurrency},
		{elapsed: 5 * time.Second, timeout: 100 * time.Second, want: maxMultipartUploadConcurrency},
		{elapsed: 20 * time.Second, timeout: 100 * time.Second, want: 2},
		{elapsed: 42 * time.Second, timeout: 100 * time.Second, want: 1},
		{elapsed: 90 * time.Second, timeout: 100 * time.Second, want: 1},
	} {
		if got := partParallelism(test.elapsed, test.timeout); got != test.want {
			t.Errorf("partParallelism(%s, %s) = %d, want %d", test.elapsed, test.timeout, got, test.want)
		}
	}
}
