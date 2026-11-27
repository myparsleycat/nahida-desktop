package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"nahida.live/desktop/internal/provenance"
)

const maxAttestationBytes = 1 << 20

// packageAttestation is the GitHub build provenance a package source publishes with each release, as a Sigstore
// bundle asset beside the package.
type packageAttestation struct {
	build provenance.Build
	// bundleFormat names the bundle asset of a version.
	bundleFormat string
	// versionSubject is the file the release workflow attests to hold the release version.
	versionSubject string
}

// provenanceClaim is what the build provenance of a downloaded file must attest.
type provenanceClaim struct {
	build  provenance.Build
	asset  string
	digest []byte
	// versionSubject, when set, names the attested file that holds version.
	versionSubject string
	version        string
	// subjects are other files the same statement must attest, as SHA-256 digests by name.
	subjects map[string][]byte
}

// UseProvenance sets the Sigstore trust that downloaded files' build provenance is verified against.
func (x *XXMI) UseProvenance(trust *provenance.Trust) {
	x.provenanceTrust = trust
}

// verifyBuildProvenance checks that one of bundles is build provenance for claim.
func (x *XXMI) verifyBuildProvenance(ctx context.Context, claim provenanceClaim, bundles []json.RawMessage) error {
	if len(bundles) == 0 {
		return fmt.Errorf("%w: %s has no attestation", provenance.ErrUnverified, claim.asset)
	}
	if x.provenanceTrust == nil {
		return errors.New("build provenance verification is not configured")
	}
	trusted, err := x.provenanceTrust.Load(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%w: %w", provenance.ErrUnverified, err)
	}

	var errs []error
	for _, bundle := range bundles {
		statement, err := provenance.Verify(trusted, claim.build, bundle, claim.digest)
		if err == nil && claim.versionSubject != "" {
			err = provenance.RequireRelease(statement, claim.asset, claim.digest, claim.versionSubject, claim.version)
		} else if err == nil {
			err = provenance.RequireSubject(statement, claim.asset, claim.digest)
		}
		for name, digest := range claim.subjects {
			if err != nil {
				break
			}
			err = provenance.RequireSubject(statement, name, digest)
		}
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
