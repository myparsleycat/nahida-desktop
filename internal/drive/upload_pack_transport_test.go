package drive

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUploadPackSendsManifestAndCreditsLogicalFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/uploads:pack" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if err := request.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Entries []struct {
				IntentID     string `json:"intentId"`
				Token        string `json:"token"`
				SHA256       string `json:"sha256"`
				PayloadBytes int64  `json:"payloadBytes"`
				CompAlg      string `json:"compAlg"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(request.FormValue("manifest")), &manifest); err != nil {
			t.Fatal(err)
		}
		if len(manifest.Entries) != 2 || manifest.Entries[0].Token != "token-1" || manifest.Entries[1].CompAlg != "zstd" {
			t.Fatalf("manifest = %+v", manifest)
		}
		file, header, err := request.FormFile("pack")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		payload, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if header.Filename != "pack.bin" || string(payload) != "abXYZ" {
			t.Fatalf("filename = %q, payload = %q", header.Filename, payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"intentId":"one","status":"completed"},{"intentId":"two","status":"completed"}]}`)
	}))
	defer server.Close()
	members := []preparedUpload{
		{
			upload:       uploadPlanEntry("one", server.URL+"/v2/uploads/one", "token-1", "hash-1"),
			source:       FinalUploadFile{UploadFile: UploadFile{FID: "file-1", Name: "one.ini", Size: 100}},
			data:         []byte("ab"),
			payloadBytes: 2,
			logicalSize:  100,
		},
		{
			upload:       uploadPlanEntry("two", server.URL+"/v2/uploads/two", "token-2", "hash-2"),
			source:       FinalUploadFile{UploadFile: UploadFile{FID: "file-2", Name: "two.ini", Size: 50}},
			data:         []byte("XYZ"),
			compression:  "zstd",
			payloadBytes: 3,
			logicalSize:  50,
		},
	}
	progress := int64(0)
	ready := make([]string, 0, 2)
	if err := uploadTestDrive(server).uploadPack(context.Background(), members, func(bytes int64) {
		progress += bytes
	}, func(file FinalUploadFile, _ []FinalUploadFile) {
		ready = append(ready, file.FID)
	}); err != nil {
		t.Fatal(err)
	}
	if progress != 150 || len(ready) != 2 || ready[0] != "file-1" || ready[1] != "file-2" {
		t.Fatalf("progress = %d, ready = %v", progress, ready)
	}
}

func uploadPlanEntry(intentID, rawURL, token, sha256 string) UploadPlanEntry {
	entry := UploadPlanEntry{IntentID: intentID, URL: rawURL}
	entry.Form.Token = token
	entry.Form.SHA256 = sha256
	return entry
}
