package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	intoto "github.com/in-toto/attestation/go/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

// The fixture is the public-good SLSA provenance that cli/cli's release workflow published for
// gh_2.105.0_windows_amd64.zip, with the Sigstore public-good trusted root fetched alongside it. Verification
// evaluates the certificate at the transparency log's integrated time, so the fixture does not expire.
const ghZipDigest = "8813a72c899900afaa10e3fec8fbc64b908c89dbfe837e95ed2fa500fc140627"

var ghReleaseBuild = buildIdentity{
	repository: "https://github.com/cli/cli",
	workflow:   ".github/workflows/deployment.yml",
	refs:       []string{"refs/heads/trunk"},
}

func TestVerifyProvenance(t *testing.T) {
	t.Parallel()
	trustedJSON, err := os.ReadFile("testdata/sigstore-trusted-root.json")
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := root.NewTrustedRootFromJSON(trustedJSON)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile("testdata/gh-provenance.sigstore.json")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := hex.DecodeString(ghZipDigest)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), digest...)
	tampered[0] ^= 1

	for _, test := range []struct {
		name   string
		build  buildIdentity
		bundle []byte
		digest []byte
		ok     bool
	}{
		{name: "attested digest", build: ghReleaseBuild, bundle: bundle, digest: digest, ok: true},
		{name: "unattested digest", build: ghReleaseBuild, bundle: bundle, digest: tampered},
		{
			name: "other workflow", bundle: bundle, digest: digest,
			build: buildIdentity{repository: ghReleaseBuild.repository, workflow: ".github/workflows/other.yml",
				refs: ghReleaseBuild.refs},
		},
		{
			name: "other branch", bundle: bundle, digest: digest,
			build: buildIdentity{repository: ghReleaseBuild.repository, workflow: ghReleaseBuild.workflow,
				refs: []string{"refs/heads/main"}},
		},
		{name: "release identity", build: releaseBuild, bundle: bundle, digest: digest},
		{name: "not a bundle", build: ghReleaseBuild, bundle: []byte("{}"), digest: digest},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			statement, err := verifyProvenance(trusted, test.build, test.bundle, test.digest)
			if test.ok {
				if err != nil || statement == nil {
					t.Fatalf("statement = %v, err = %v", statement, err)
				}
				return
			}
			if !errors.Is(err, ErrProvenanceUnverified) {
				t.Fatalf("err = %v, want ErrProvenanceUnverified", err)
			}
		})
	}
}

func TestRequireReleaseSubjects(t *testing.T) {
	t.Parallel()
	subject := func(name, content string) *intoto.ResourceDescriptor {
		digest := sha256.Sum256([]byte(content))
		return &intoto.ResourceDescriptor{
			Name:   name,
			Digest: map[string]string{"sha256": hex.EncodeToString(digest[:])},
		}
	}
	release := &intoto.Statement{Subject: []*intoto.ResourceDescriptor{
		subject(assetName, "app 3.53.0"),
		subject("nahida-desktop-windows-amd64-installer.exe", "installer 3.53.0"),
		subject(versionSubject, "3.53.0"),
	}}
	digestOf := func(content string) []byte {
		digest := sha256.Sum256([]byte(content))
		return digest[:]
	}

	for _, test := range []struct {
		name    string
		version string
		digest  []byte
		ok      bool
	}{
		{name: "this release", version: "3.53.0", digest: digestOf("app 3.53.0"), ok: true},
		{name: "older release under a newer tag", version: "3.54.0", digest: digestOf("app 3.53.0")},
		{name: "installer as the executable", version: "3.53.0", digest: digestOf("installer 3.53.0")},
		{name: "unattested executable", version: "3.53.0", digest: digestOf("other build")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := requireReleaseSubjects(release, test.version, test.digest)
			if test.ok != (err == nil) || err != nil && !errors.Is(err, ErrProvenanceUnverified) {
				t.Fatalf("err = %v, want ok = %t", err, test.ok)
			}
		})
	}
}

func TestTUFFetcherReportsStatusAndSize(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 16)))
	}))
	t.Cleanup(server.Close)
	noRetries := 0
	fetcher := tufFetcher{ctx: t.Context(), http: infra.NewClientWithOptions(infra.ClientOptions{
		Status: infra.BackendOnline, RetryLimit: &noRetries,
	})}

	// go-tuf stops looking for newer root versions only on an HTTP 404 reported as ErrDownloadHTTP.
	var httpErr *metadata.ErrDownloadHTTP
	if _, err := fetcher.DownloadFile(server.URL+"/missing", 64, 0); !errors.As(err, &httpErr) ||
		httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file err = %v, want ErrDownloadHTTP 404", err)
	}
	var sizeErr *metadata.ErrDownloadLengthMismatch
	if _, err := fetcher.DownloadFile(server.URL+"/file", 15, 0); !errors.As(err, &sizeErr) {
		t.Fatalf("oversized file err = %v, want ErrDownloadLengthMismatch", err)
	}
	if data, err := fetcher.DownloadFile(server.URL+"/file", 16, 0); err != nil || len(data) != 16 {
		t.Fatalf("file = %d bytes, err = %v", len(data), err)
	}
}

func TestLoadTrustPreservesRotatedRootAfterOnlineTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cacheDir := t.TempDir()
		rotated, _ := seedTrustCache(t, cacheDir)
		restarted := false
		var requests []string
		transport := trustTestTransport(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.URL.Path)
			res := httptest.NewRecorder()
			switch req.URL.Path {
			case "/2.root.json":
				_, _ = res.Write(rotated)
			case "/3.root.json":
				if restarted {
					res.WriteHeader(http.StatusServiceUnavailable)
				} else {
					res.WriteHeader(http.StatusNotFound)
				}
			case "/timestamp.json":
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(30 * time.Second):
					return nil, context.DeadlineExceeded
				}
			default:
				t.Errorf("unexpected request: %s", req.URL.Path)
				res.WriteHeader(http.StatusNotFound)
			}
			return res.Result(), nil
		})
		noRetries := 0
		client := infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: &http.Client{Transport: transport}, Status: infra.BackendOnline, RetryLimit: &noRetries,
		})
		trust := newSigstoreTrust(client, cacheDir, nil)

		start := time.Now()
		if _, err := trust.load(t.Context()); err != nil {
			t.Errorf("load trust from cache: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Errorf("load trust took %s with a stalled mirror", elapsed)
		}
		if len(requests) != 3 {
			t.Errorf("requests = %v, want one online refresh", requests)
		}
		restarted = true
		requests = nil
		if _, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context()); err != nil {
			t.Errorf("load trust after restart: %v", err)
		}
		if len(requests) != 1 || requests[0] != "/3.root.json" {
			t.Errorf("requests after restart = %v, want [/3.root.json]", requests)
		}
	})
}

func TestLoadTrustRejectsStaleSnapshotAfterTimestampUpdate(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	_, updated := seedTrustCache(t, cacheDir)
	var offline, snapshotAvailable, requestedSnapshot bool
	transport := trustTestTransport(func(req *http.Request) (*http.Response, error) {
		res := httptest.NewRecorder()
		path := req.URL.Path
		switch {
		case offline:
			res.WriteHeader(524)
		case path == "/2.root.json":
			res.WriteHeader(http.StatusNotFound)
		case path == "/2.snapshot.json" && !snapshotAvailable:
			requestedSnapshot = true
			res.WriteHeader(http.StatusServiceUnavailable)
		case updated[path] != nil:
			_, _ = res.Write(updated[path])
		default:
			t.Errorf("unexpected request: %s", path)
			res.WriteHeader(http.StatusNotFound)
		}
		return res.Result(), nil
	})
	client := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: transport}, Status: infra.BackendOnline, RetryLimit: new(int),
	})

	for i, wantStatus := range []int{http.StatusServiceUnavailable, 524} {
		trusted, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context())
		status, ok := errors.AsType[*metadata.ErrDownloadHTTP](err)
		if trusted != nil || !ok || status.StatusCode != wantStatus {
			t.Errorf("attempt %d: loaded trust = %t, err = %v, want HTTP %d", i+1, trusted != nil, err, wantStatus)
		}
		offline = true
	}
	if !requestedSnapshot {
		t.Error("the mirror was never asked for the new snapshot")
	}
	offline, snapshotAvailable = false, true
	if _, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context()); err != nil {
		t.Errorf("load trust after mirror recovery: %v", err)
	}
}

func TestLoadTrustRejectsCachedTargetAfterTargetsUpdate(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	_, updated := seedTrustCache(t, cacheDir)
	var mirrorOffline, targetAvailable, requestedTarget bool
	transport := trustTestTransport(func(req *http.Request) (*http.Response, error) {
		res := httptest.NewRecorder()
		path := req.URL.Path
		switch {
		case mirrorOffline:
			res.WriteHeader(http.StatusServiceUnavailable)
		case path == "/2.root.json":
			res.WriteHeader(http.StatusNotFound)
		case updated[path] == nil:
			t.Errorf("unexpected request: %s", path)
			res.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(path, "/targets/") && !targetAvailable:
			requestedTarget = true
			res.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = res.Write(updated[path])
		}
		return res.Result(), nil
	})
	client := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: transport}, Status: infra.BackendOnline, RetryLimit: new(int),
	})

	for _, phase := range []string{"after targets update", "after restart"} {
		trusted, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context())
		status, ok := errors.AsType[*metadata.ErrDownloadHTTP](err)
		if trusted != nil || !ok || status.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s: loaded trust = %t, err = %v", phase, trusted != nil, err)
		}
		mirrorOffline = true
	}
	if !requestedTarget {
		t.Error("the mirror was never asked for the updated trust target")
	}
	mirrorOffline, targetAvailable = false, true
	if _, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context()); err != nil {
		t.Errorf("load trust after mirror recovery: %v", err)
	}
}

func TestLoadTrustFallsBackAfterTruncatedResponse(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	seedTrustCache(t, cacheDir)
	transport := trustTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/2.root.json" {
			t.Errorf("unexpected request: %s", req.URL.Path)
		}
		response := httptest.NewRecorder().Result()
		response.Body = io.NopCloser(io.MultiReader(
			strings.NewReader(`{"signed":`), iotest.ErrReader(io.ErrUnexpectedEOF),
		))
		return response, nil
	})
	client := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: transport}, Status: infra.BackendOnline, RetryLimit: new(int),
	})
	if trusted, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context()); trusted == nil || err != nil {
		t.Fatalf("load from cache after truncated response: loaded trust = %t, err = %v", trusted != nil, err)
	}
}

func TestReadTUFCacheRecoversFromCorruptJSON(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	mirror := tufMirrorPath()
	if err := os.MkdirAll(filepath.Join(cacheDir, mirror), 0o700); err != nil {
		t.Fatal(err)
	}
	const savedRoot = "saved TUF root"
	if err := os.WriteFile(filepath.Join(cacheDir, mirror, "root.json"), []byte(savedRoot), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, tufCacheFile), []byte(`{"latest":`), 0o600); err != nil {
		t.Fatal(err)
	}

	cache, err := newSigstoreTrust(nil, cacheDir, nil).readTUFCache(t.Context())
	if got := string(cache.Latest[mirror+"/root.json"]); err != nil || got != savedRoot {
		t.Fatalf("cached root = %q, err = %v, want %q", got, err, savedRoot)
	}
}

func TestLoadTrustFallsBackAfterForbiddenResponse(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	seedTrustCache(t, cacheDir)
	stale := filepath.Join(cacheDir, ".tuf-stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	transport := trustTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/2.root.json" {
			t.Errorf("unexpected request: %s", req.URL.Path)
		}
		response := httptest.NewRecorder()
		response.WriteHeader(http.StatusForbidden)
		return response.Result(), nil
	})
	client := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: transport}, Status: infra.BackendOnline, RetryLimit: new(int),
	})

	if trusted, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context()); trusted == nil || err != nil {
		t.Errorf("load from cache after forbidden response: loaded trust = %t, err = %v", trusted != nil, err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale TUF staging directory survived: %v", err)
	}
}

func TestCheckPreservesCancellationDuringTrustRefresh(t *testing.T) {
	t.Parallel()
	const tag = "v3.46.0"
	server := &releaseServer{
		releases: []github.Release{testRelease(tag, false, assetName, checksumAsset, bundleAsset)},
		files:    map[string]string{},
	}
	server.setFile(tag, checksumAsset, checksumLine("released build", assetName))
	server.setFile(tag, bundleAsset, attestation("released build", "3.46.0"))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cacheDir := t.TempDir()
	rotated, _ := seedTrustCache(t, cacheDir)
	restarted := false
	var restartRequests []string
	transport := trustTestTransport(func(req *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(req.URL.String(), tuf.DefaultMirror+"/") {
			return server.RoundTrip(req)
		}
		response := httptest.NewRecorder()
		if restarted {
			restartRequests = append(restartRequests, req.URL.Path)
			response.WriteHeader(http.StatusServiceUnavailable)
			return response.Result(), nil
		}
		switch req.URL.Path {
		case "/2.root.json":
			_, _ = response.Write(rotated)
		case "/3.root.json":
			response.WriteHeader(http.StatusNotFound)
		case "/timestamp.json":
			cancel()
			return nil, req.Context().Err()
		default:
			t.Errorf("unexpected request: %s", req.URL.Path)
			response.WriteHeader(http.StatusNotFound)
		}
		return response.Result(), nil
	})
	client := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: transport}, Status: infra.BackendOnline, RetryLimit: new(int),
	})
	engine, err := New(Options{
		Version: "3.45.0", GitHub: github.New(github.Options{HTTP: client}), HTTP: client, TrustCacheDir: cacheDir,
		Quit: func() { t.Error("unexpected quit") },
	})
	if err != nil {
		t.Fatal(err)
	}

	release, err := engine.Check(ctx, false)
	if release != nil || !errors.Is(err, context.Canceled) || !infra.IsCancellationError(err) {
		t.Fatalf("release = %+v, err = %v, want cancellation", release, err)
	}

	restarted = true
	if _, err := newSigstoreTrust(client, cacheDir, nil).load(t.Context()); err != nil {
		t.Fatalf("load trust after restart: %v", err)
	}
	if len(restartRequests) != 1 || restartRequests[0] != "/3.root.json" {
		t.Errorf("requests after restart = %v, want [/3.root.json]", restartRequests)
	}
}

type trustTestTransport func(*http.Request) (*http.Response, error)

func (f trustTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func seedTrustCache(t *testing.T, cacheDir string) (rotated []byte, updated map[string][]byte) {
	t.Helper()
	const targetName = "trusted_root.json"
	target, err := os.ReadFile("testdata/sigstore-trusted-root.json")
	if err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(cacheDir, tuf.URLToPath(tuf.DefaultMirror))
	if err := os.MkdirAll(filepath.Join(mirror, "targets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mirror, "targets", targetName), target, 0o600); err != nil {
		t.Fatal(err)
	}

	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := signature.LoadSigner(private, 0)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(m interface {
		Sign(signature.Signer) (*metadata.Signature, error)
		ToBytes(bool) ([]byte, error)
	}) []byte {
		if _, err := m.Sign(signer); err != nil {
			t.Fatal(err)
		}
		data, err := m.ToBytes(false)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	key, err := metadata.KeyFromPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	initial := metadata.Root(expires)
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		if err := initial.Signed.AddKey(key, role); err != nil {
			t.Fatal(err)
		}
	}
	targets := metadata.Targets(expires)
	targets.Signed.Targets[targetName], err = metadata.TargetFile().FromBytes(targetName, target, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"root.json":      encode(initial),
		"timestamp.json": encode(metadata.Timestamp(expires)),
		"snapshot.json":  encode(metadata.Snapshot(expires)),
		"targets.json":   encode(targets),
	} {
		if err := os.WriteFile(filepath.Join(mirror, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	initial.Signed.Version++
	initial.ClearSignatures()
	timestamp := metadata.Timestamp(expires)
	timestamp.Signed.Version++
	timestamp.Signed.Meta["snapshot.json"] = metadata.MetaFile(2)
	snapshot := metadata.Snapshot(expires)
	snapshot.Signed.Version++
	snapshot.Signed.Meta["targets.json"] = metadata.MetaFile(2)
	target = append(target, '\n')
	descriptor, err := metadata.TargetFile().FromBytes(targetName, target, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	targets.Signed.Version++
	targets.Signed.Targets[targetName] = descriptor
	targets.ClearSignatures()
	targetPath := "/targets/" + hex.EncodeToString(descriptor.Hashes["sha256"]) + "." + targetName
	return encode(initial), map[string][]byte{
		"/timestamp.json":  encode(timestamp),
		"/2.snapshot.json": encode(snapshot),
		"/2.targets.json":  encode(targets),
		targetPath:         target,
	}
}
