package drive

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunUploadTasksHonorsConfiguredConcurrency(t *testing.T) {
	var active atomic.Int32
	var peak atomic.Int32
	tasks := make([]func(), 6)
	for index := range tasks {
		tasks[index] = func() {
			current := active.Add(1)
			for current > peak.Load() && !peak.CompareAndSwap(peak.Load(), current) {
			}
			time.Sleep(20 * time.Millisecond)
			active.Add(-1)
		}
	}
	if err := runUploadTasks(context.Background(), 3, tasks); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 3 {
		t.Fatalf("peak concurrency = %d, want 3", peak.Load())
	}
}

func TestUploadTaskPoolStartsWorkBeforePlanningFinishes(t *testing.T) {
	t.Parallel()
	pool := newUploadTaskPool(context.Background(), 1)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := pool.Submit(func() {
		close(started)
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued upload did not start until pool close")
	}
	close(release)
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUploadPackAndMultipartLimitsMatchElectron(t *testing.T) {
	t.Parallel()
	if maxMultipartUploadConcurrency != 4 {
		t.Fatalf("multipart concurrency = %d, want 4", maxMultipartUploadConcurrency)
	}
	pack := testUploadRules().Pack
	if shouldFlushUploadPack(pack.MaxFiles-1, pack.PayloadBudget-1, pack) {
		t.Fatal("pack flushed below both limits")
	}
	if !shouldFlushUploadPack(pack.MaxFiles, 1, pack) || !shouldFlushUploadPack(1, pack.PayloadBudget, pack) {
		t.Fatal("pack did not flush at an Electron limit")
	}
}

func TestRedistributeUploadFilesInterleavesLargeFiles(t *testing.T) {
	const large = int64(50 * 1024 * 1024)
	files := []FinalUploadFile{
		{UploadFile: UploadFile{FID: "large-1", Size: large}},
		{UploadFile: UploadFile{FID: "large-2", Size: large + 1}},
		{UploadFile: UploadFile{FID: "small-1", Size: 1}},
		{UploadFile: UploadFile{FID: "small-2", Size: 2}},
		{UploadFile: UploadFile{FID: "small-3", Size: 3}},
		{UploadFile: UploadFile{FID: "small-4", Size: 4}},
	}
	ordered := redistributeUploadFiles(files)
	got := make([]string, len(ordered))
	for index, file := range ordered {
		got[index] = file.FID
	}
	want := []string{"small-1", "small-2", "large-1", "small-3", "small-4", "large-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestExecuteUploadPlanPacksSmallNonBundleIntents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/uploads:pack" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if err := request.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"results":[{"intentId":"one","status":"completed"},{"intentId":"two","status":"completed"}]}`)
	}))
	defer server.Close()
	directory := t.TempDir()
	files := []FinalUploadFile{
		uploadExecutionFile(t, directory, "file-1", "one.ini", "ab"),
		uploadExecutionFile(t, directory, "file-2", "two.ini", "xyz"),
	}
	plan := UploadPlan{
		Items: []UploadPlanItem{
			{ClientID: "file-1", Status: "pending", IntentID: "one"},
			{ClientID: "file-2", Status: "pending", IntentID: "two"},
		},
		Uploads: map[string]UploadPlanEntry{
			"one": uploadPlanEntry("one", server.URL+"/v2/uploads/one", "token-1", "hash-1"),
			"two": uploadPlanEntry("two", server.URL+"/v2/uploads/two", "token-2", "hash-2"),
		},
		Bundles: map[string]NTEBundle{},
	}
	var bytes int64
	completed := make([]string, 0, 2)
	err := uploadTestDrive(server).executeUploadPlanV2(context.Background(), files, plan, 8, func(progress UploadExecutionProgress) {
		bytes += progress.Bytes
		if progress.FileID != "" {
			completed = append(completed, progress.FileID)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(completed)
	if bytes != 5 || !slices.Equal(completed, []string{"file-1", "file-2"}) {
		t.Fatalf("bytes = %d, completed = %v", bytes, completed)
	}
}

func TestExecuteUploadPlanUsesPartsWhenDirectBodyExceedsLimit(t *testing.T) {
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
	directory := t.TempDir()
	files := []FinalUploadFile{uploadExecutionFile(t, directory, "file-1", "one.ini", "ab")}
	plan := UploadPlan{
		Items: []UploadPlanItem{{ClientID: "file-1", Status: "pending", IntentID: "one"}},
		Uploads: map[string]UploadPlanEntry{
			"one": uploadPlanEntry("one", server.URL+"/v2/uploads/one", "token-1", "hash-1"),
		},
		Bundles: map[string]NTEBundle{},
	}
	drive := uploadTestDrive(server)
	rules := testUploadRules()
	rules.MaxUploadBodyBytes = 32
	drive.setUploadRules(rules)
	if err := drive.executeUploadPlanV2(context.Background(), files, plan, 8, nil); err != nil {
		t.Fatal(err)
	}
	if partRequests.Load() != 1 {
		t.Fatalf("part requests = %d, want 1", partRequests.Load())
	}
}

func TestExecuteUploadPlanCompletesNTEBundleAtomically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/bundle/complete" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	files := []FinalUploadFile{
		{UploadFile: UploadFile{FID: "pak", Name: "mod.pak", Size: 10}},
		{UploadFile: UploadFile{FID: "utoc", Name: "mod.utoc", Size: 20}},
		{UploadFile: UploadFile{FID: "ucas", Name: "mod.ucas", Size: 30}},
	}
	bundle := NTEBundle{ID: "bundle", MemberClientIDs: []string{"pak", "utoc", "ucas"}, CompleteURL: server.URL + "/bundle/complete"}
	bundle.Form.Token = "token"
	plan := UploadPlan{
		Items: []UploadPlanItem{
			{ClientID: "pak", Status: "exists", BundleID: "bundle"},
			{ClientID: "utoc", Status: "exists", BundleID: "bundle"},
			{ClientID: "ucas", Status: "exists", BundleID: "bundle"},
		},
		Uploads: map[string]UploadPlanEntry{},
		Bundles: map[string]NTEBundle{"bundle": bundle},
	}
	var bytes int64
	completed := make([]string, 0, 3)
	if err := uploadTestDrive(server).executeUploadPlanV2(context.Background(), files, plan, 8, func(progress UploadExecutionProgress) {
		bytes += progress.Bytes
		if progress.FileID != "" {
			completed = append(completed, progress.FileID)
		}
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(completed)
	if bytes != 60 || !slices.Equal(completed, []string{"pak", "ucas", "utoc"}) {
		t.Fatalf("bytes = %d, completed = %v", bytes, completed)
	}
}

func TestExecuteUploadPlanAbortsAndRollsBackFailedNTEBundle(t *testing.T) {
	aborted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/bundle/complete":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"code":"nte_invalid","reason":"nte_invalid"}`)
		case "/bundle/abort":
			aborted = true
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	files := []FinalUploadFile{{UploadFile: UploadFile{FID: "pak", Name: "mod.pak", Size: 10}}}
	bundle := NTEBundle{
		ID:              "bundle",
		MemberClientIDs: []string{"pak"},
		CompleteURL:     server.URL + "/bundle/complete",
		AbortURL:        server.URL + "/bundle/abort",
	}
	bundle.Form.Token = "token"
	plan := UploadPlan{
		Items:   []UploadPlanItem{{ClientID: "pak", Status: "exists", BundleID: "bundle"}},
		Uploads: map[string]UploadPlanEntry{},
		Bundles: map[string]NTEBundle{"bundle": bundle},
	}
	var bytes int64
	completed := 0
	err := uploadTestDrive(server).executeUploadPlanV2(context.Background(), files, plan, 8, func(progress UploadExecutionProgress) {
		bytes += progress.Bytes
		if progress.FileID != "" {
			completed++
		}
	})
	var uploadErr *UploadV2Error
	if !errors.As(err, &uploadErr) || uploadErr.Code != "nte_invalid" {
		t.Fatalf("error = %v", err)
	}
	if !aborted || bytes != 0 || completed != 0 {
		t.Fatalf("aborted = %v, bytes = %d, completed = %d", aborted, bytes, completed)
	}
}

func uploadExecutionFile(t *testing.T, directory, id, name, content string) FinalUploadFile {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return FinalUploadFile{UploadFile: UploadFile{FID: id, Name: name, Size: int64(len(content)), FullPath: filepath.ToSlash(path)}}
}
