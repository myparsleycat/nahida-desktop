// Package provenance verifies GitHub artifact attestations: Sigstore bundles of SLSA build provenance that a
// GitHub Actions workflow signed for a release's assets.
package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	intoto "github.com/in-toto/attestation/go/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/theupdateframework/go-tuf/v2/metadata"

	"nahida.live/desktop/internal/infra"
)

const (
	actionsIssuer       = "https://token.actions.githubusercontent.com"
	slsaProvenanceType  = "https://slsa.dev/provenance/v1"
	githubHostedRunner  = "github-hosted"
	trustRootCacheDays  = 1
	trustRefreshTimeout = 10 * time.Second
)

var ErrUnverified = errors.New("build provenance could not be verified")

// Build is the GitHub Actions workflow that must have signed a release's provenance.
type Build struct {
	// Repository is the repository URL, such as https://github.com/owner/name.
	Repository string
	// Workflow is the workflow file path in the repository.
	Workflow string
	// Refs are the git refs the workflow may have run on.
	Refs []string
}

func (b Build) certificateIdentity() (verify.CertificateIdentity, error) {
	refs := make([]string, len(b.Refs))
	for i, ref := range b.Refs {
		refs[i] = regexp.QuoteMeta(ref)
	}
	san := "^" + regexp.QuoteMeta(b.Repository+"/"+b.Workflow+"@") + "(?:" + strings.Join(refs, "|") + ")$"
	sanMatcher, err := verify.NewSANMatcher("", san)
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	issuerMatcher, err := verify.NewIssuerMatcher(actionsIssuer, "")
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	return verify.NewCertificateIdentity(sanMatcher, issuerMatcher, certificate.Extensions{
		SourceRepositoryURI: b.Repository,
		RunnerEnvironment:   githubHostedRunner,
	})
}

// Verify checks that bundleJSON is a Sigstore bundle whose SLSA provenance statement lists a subject with the
// SHA-256 digest and was signed by build, with a Fulcio certificate, a certificate transparency timestamp and a
// transparency log entry that trusted accepts, and returns that statement.
func Verify(trusted root.TrustedMaterial, build Build, bundleJSON, digest []byte) (*intoto.Statement, error) {
	var entity bundle.Bundle
	if err := entity.UnmarshalJSON(bytes.TrimSpace(bundleJSON)); err != nil {
		return nil, fmt.Errorf("%w: parse bundle: %w", ErrUnverified, err)
	}
	identity, err := build.certificateIdentity()
	if err != nil {
		return nil, fmt.Errorf("%w: build identity: %w", ErrUnverified, err)
	}
	verifier, err := verify.NewVerifier(trusted,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: create verifier: %w", ErrUnverified, err)
	}
	result, err := verifier.Verify(&entity, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digest),
		verify.WithCertificateIdentity(identity),
	))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	if result.Statement == nil || result.Statement.GetPredicateType() != slsaProvenanceType {
		return nil, fmt.Errorf("%w: statement is not SLSA provenance", ErrUnverified)
	}
	return result.Statement, nil
}

// RequireRelease checks that statement attests digest under the asset name together with a file named
// versionSubject that holds version, so a build attested for another release or another asset is rejected.
func RequireRelease(statement *intoto.Statement, asset string, digest []byte, versionSubject, version string) error {
	if err := RequireSubject(statement, asset, digest); err != nil {
		return err
	}
	versionDigest := sha256.Sum256([]byte(version))
	if !hasSubject(statement, versionSubject, versionDigest[:]) {
		return fmt.Errorf("%w: statement does not attest release %s", ErrUnverified, version)
	}
	return nil
}

// RequireSubject checks that statement attests digest under the asset name.
func RequireSubject(statement *intoto.Statement, asset string, digest []byte) error {
	if !hasSubject(statement, asset, digest) {
		return fmt.Errorf("%w: statement does not attest %s with this digest", ErrUnverified, asset)
	}
	return nil
}

func hasSubject(statement *intoto.Statement, name string, digest []byte) bool {
	for _, subject := range statement.GetSubject() {
		if subject.GetName() != name {
			continue
		}
		got, err := hex.DecodeString(subject.GetDigest()["sha256"])
		if err == nil && bytes.Equal(got, digest) {
			return true
		}
	}
	return false
}

// Trust loads the Sigstore public-good trusted root through TUF and caches the verified metadata in its cache
// directory. Loads of one Trust are serialized because they share the cache; separate Trusts on one directory are
// not.
type Trust struct {
	http     *infra.Client
	cacheDir string
	log      *infra.Log
	slot     chan struct{}
}

func NewTrust(http *infra.Client, cacheDir string, log *infra.Log) *Trust {
	return &Trust{http: http, cacheDir: cacheDir, log: log, slot: make(chan struct{}, 1)}
}

func (s *Trust) Load(ctx context.Context) (*root.TrustedRoot, error) {
	select {
	case s.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.slot }()

	s.sweepTUFStaging(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache, err := s.readTUFCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("read Sigstore trust cache: %w", err)
	}
	workDir, err := stageTUFFiles(ctx, s.cacheDir, cache.Latest)
	if err != nil {
		return nil, fmt.Errorf("stage Sigstore trust cache: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	refreshCtx, cancel := context.WithTimeout(ctx, trustRefreshTimeout)
	trusted, refreshErr := s.loadRoot(refreshCtx, workDir, false)
	cancel()

	// TUF may have advanced its rollback state before the caller canceled the refresh.
	cacheCtx, cancelCache := context.WithTimeout(context.WithoutCancel(ctx), trustRefreshTimeout)
	latest, err := readTUFFiles(cacheCtx, workDir)
	cancelCache()
	if err != nil {
		return nil, errors.Join(refreshErr, fmt.Errorf("read refreshed Sigstore trust cache: %w", err))
	}
	if !maps.EqualFunc(cache.Latest, latest, bytes.Equal) {
		next := tufCache{Latest: latest}
		if err := next.write(s.cacheDir); err != nil {
			s.warn(fmt.Errorf("write Sigstore trust cache: %w", err), "trust-cache",
				map[string]any{"directory": s.cacheDir})
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if refreshErr == nil || !sigstoreMirrorUnavailable(refreshErr) || len(cache.Latest) == 0 {
		return trusted, refreshErr
	}

	cached, err := s.loadRoot(ctx, workDir, true)
	if err != nil {
		return nil, errors.Join(refreshErr, fmt.Errorf("load cached Sigstore trust: %w", err))
	}
	return cached, nil
}

func (s *Trust) loadRoot(ctx context.Context, cacheDir string, cacheOnly bool) (*root.TrustedRoot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	trustRoot, err := os.ReadFile(filepath.Join(cacheDir, tufMirrorPath(), "root.json"))
	if errors.Is(err, os.ErrNotExist) && !cacheOnly {
		trustRoot = tuf.DefaultRoot()
	} else if err != nil {
		return nil, fmt.Errorf("read cached Sigstore TUF root: %w", err)
	}
	client, err := tuf.New(&tuf.Options{
		Root:              trustRoot,
		CachePath:         cacheDir,
		CacheValidity:     trustRootCacheDays,
		ForceCache:        cacheOnly,
		RepositoryBaseURL: tuf.DefaultMirror,
		Fetcher:           tufFetcher{ctx: ctx, http: s.http, cacheOnly: cacheOnly},
	})
	if err != nil {
		return nil, fmt.Errorf("update Sigstore trust metadata: %w", err)
	}
	trusted, err := root.GetTrustedRoot(client)
	if err != nil {
		return nil, fmt.Errorf("read Sigstore trusted root: %w", err)
	}
	return trusted, nil
}

func (s *Trust) warn(err error, stage string, fields map[string]any) {
	_ = infra.ReportError(s.log, err, "Provenance", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "provenance-trust", Stage: stage, Fields: fields,
	})
}

func sigstoreMirrorUnavailable(err error) bool {
	if status, ok := errors.AsType[*metadata.ErrDownloadHTTP](err); ok {
		switch status.StatusCode {
		case http.StatusForbidden, http.StatusProxyAuthRequired, http.StatusRequestTimeout, http.StatusTooManyRequests:
			return true
		default:
			return status.StatusCode >= http.StatusInternalServerError
		}
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || infra.IsUnreachable(err)
}

// tufFetcher sends TUF requests through the application's HTTP client. go-tuf ends its root version walk on an
// HTTP 404, so every status other than 200 must surface as metadata.ErrDownloadHTTP.
type tufFetcher struct {
	// go-tuf's Fetcher interface takes no context, so each load builds a fetcher bound to its own.
	ctx       context.Context
	http      *infra.Client
	cacheOnly bool
}

func (f tufFetcher) DownloadFile(rawURL string, maxLength int64, _ time.Duration) ([]byte, error) {
	// ForceCache still attempts downloads if metadata is expired or a target is missing.
	if f.cacheOnly {
		return nil, errors.New("trust metadata download disabled during cache fallback")
	}
	response, err := f.http.Fetch(f.ctx, rawURL, infra.FetchOptions{
		DisableHTTPErrors: true, DisableTimeoutRewrite: true,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: response.StatusCode, URL: rawURL}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxLength+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{
			Msg: fmt.Sprintf("%s is larger than %d bytes", rawURL, maxLength),
		}
	}
	return data, nil
}
