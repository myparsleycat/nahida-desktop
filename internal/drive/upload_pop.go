package drive

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"net/http"
	"slices"

	"github.com/google/uuid"
)

const (
	uploadPopVersion    = "akasha-pop-v1"
	uploadPopCapability = "upload-pop-v1"
	uploadPopKeyPrefix  = uploadPopVersion + "\x00"
	uploadPopProofLabel = uploadPopVersion + "/proof\x00"
	uploadPopNonceSize  = 32
	uploadProveBatch    = 500
)

// UploadChallenge is the proof-of-possession challenge a plan attaches to an
// upload entry. It says nothing about whether the server holds the content.
type UploadChallenge struct {
	Version  string `json:"version"`
	Nonce    string `json:"nonce"`
	ProveURL string `json:"proveUrl"`
}

// uploadPlanCapabilities is what every plan request announces. Announcing
// upload-pop-v1 stops the server from reusing content by its hash alone, so
// this client must answer the challenges or upload the bytes of everything.
func uploadPlanCapabilities() []string {
	return []string{"nte-bundle-v1", uploadPopCapability}
}

// newUploadPopHash starts the proof-of-possession key of one content; the
// logical bytes are written to it in the same pass as the content hash.
func newUploadPopHash() hash.Hash {
	digest := sha256.New()
	_, _ = digest.Write([]byte(uploadPopKeyPrefix))
	return digest
}

func uploadPopProof(key []byte, intentID uuid.UUID, nonce []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(uploadPopProofLabel))
	_, _ = mac.Write([]byte(intentID.String()))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(nonce)
	return mac.Sum(nil)
}

type uploadProofClaim struct {
	IntentID string `json:"intentId"`
	Token    string `json:"token"`
	Proof    string `json:"proof"`
}

// uploadProofFor answers the proof for an entry's challenge, or false when the
// entry carries no usable challenge or no target's pop key is known.
func uploadProofFor(upload UploadPlanEntry, targets []FinalUploadFile) (uploadProofClaim, bool) {
	challenge := upload.Challenge
	if challenge == nil || challenge.Version != uploadPopVersion || challenge.ProveURL == "" {
		return uploadProofClaim{}, false
	}
	nonce, err := hex.DecodeString(challenge.Nonce)
	if err != nil || len(nonce) != uploadPopNonceSize {
		return uploadProofClaim{}, false
	}
	intentID, err := uuid.Parse(upload.IntentID)
	if err != nil {
		return uploadProofClaim{}, false
	}
	index := slices.IndexFunc(targets, func(file FinalUploadFile) bool { return len(file.PopKey) == sha256.Size })
	if index < 0 {
		return uploadProofClaim{}, false
	}
	return uploadProofClaim{
		IntentID: upload.IntentID,
		Token:    upload.Form.Token,
		Proof:    hex.EncodeToString(uploadPopProof(targets[index].PopKey, intentID, nonce)),
	}, true
}

// proveUploads sends one batch of proofs once. A challenge is spent by its
// first answer, so neither a 202 nor a failure is a reason to send it again.
func (d *Drive) proveUploads(
	ctx context.Context,
	proveURL string,
	claims []uploadProofClaim,
) ([]intentPackResult, error) {
	result, err := d.sendJSON(ctx, proveURL, map[string]any{"proofs": claims})
	if err != nil {
		return nil, err
	}
	if result.status != http.StatusOK && result.status != http.StatusAccepted {
		return nil, uploadResultError(result)
	}
	return decodeIntentPackResults(result.payload)
}

// proveIntents answers the plan's challenges before any byte is sent, and
// answers the intents it settled: completed by the proof, refused, or handed to
// the status poll. Every other intent, including those of a prove request that
// failed as a whole, is left to the byte upload.
func (r *uploadRun) proveIntents() (map[string]struct{}, error) {
	settled := make(map[string]struct{})
	claimsByURL := make(map[string][]uploadProofClaim)
	proveURLs := make([]string, 0, 1)
	for _, intentID := range r.intentOrder {
		upload, ok := r.plan.Uploads[intentID]
		if !ok {
			continue
		}
		claim, ok := uploadProofFor(upload, r.pendingByIntent[intentID])
		if !ok {
			continue
		}
		proveURL := upload.Challenge.ProveURL
		if _, seen := claimsByURL[proveURL]; !seen {
			proveURLs = append(proveURLs, proveURL)
		}
		claimsByURL[proveURL] = append(claimsByURL[proveURL], claim)
	}

	if len(proveURLs) == 0 {
		return settled, nil
	}
	r.activity.setPreparing(true)
	defer r.activity.setPreparing(false)
	for _, proveURL := range proveURLs {
		for batch := range slices.Chunk(claimsByURL[proveURL], uploadProveBatch) {
			results, err := r.drive.proveUploads(r.ctx, proveURL, batch)
			if err != nil {
				if ctxErr := r.ctx.Err(); ctxErr != nil {
					r.drive.abortAllNTEBundles(r.ctx, r.plan.Bundles)
					return nil, ctxErr
				}
				continue
			}
			for _, claim := range batch {
				result, found := uniqueIntentPackResult(results, claim.IntentID)
				if !found {
					continue
				}
				done, err := r.settleProof(claim.IntentID, result)
				if err != nil {
					return nil, err
				}
				if done {
					settled[claim.IntentID] = struct{}{}
				}
			}
		}
	}
	return settled, nil
}

// settleProof applies one prove result and reports whether it settled the
// intent. upload_required, and invalid_proof, which refuses the proof's form
// and leaves the challenge open, leave the intent to the byte upload.
func (r *uploadRun) settleProof(intentID string, result intentPackResult) (bool, error) {
	targets := r.pendingByIntent[intentID]
	switch {
	case result.Status == "completed":
		for _, file := range targets {
			r.markReady(file, file.Size, true)
		}
		return true, nil
	case result.Status == "processing" || result.NextAction == "poll":
		return true, r.queuePolledIntent(r.plan.Uploads[intentID], targets)
	case result.Status == "failed" && result.Reason != "invalid_proof":
		reason := result.Reason
		if reason == "" {
			reason = result.Status
		}
		r.failTargets(&UploadV2Error{Code: reason, Message: reason}, targets)
		return true, nil
	}
	return false, nil
}

// queuePolledIntent waits for another upload of the same intent to finish,
// and sends the bytes itself only when the server asks for them after all.
func (r *uploadRun) queuePolledIntent(upload UploadPlanEntry, targets []FinalUploadFile) error {
	taskCtx := r.targetContext(targets)
	source := targets[0]
	recoverable := r.recoverable(targets)
	return r.queueTask(func() {
		uploadRequired, err := r.drive.waitUploadIntent(taskCtx, upload, recoverable)
		if err == nil && !uploadRequired {
			for _, file := range targets {
				r.markReady(file, file.Size, true)
			}
			return
		}
		if err == nil {
			err = r.uploadAlone(taskCtx, upload, source, recoverable)
		}
		if err != nil {
			if taskCtx.Err() == nil || r.ctx.Err() != nil {
				r.failTargets(err, targets)
			}
			return
		}
		r.markIntentReady(source, targets[1:])
	})
}

// uploadAlone sends one intent's bytes outside a pack, directly or in parts by
// the same rules dispatchIntent applies.
func (r *uploadRun) uploadAlone(
	ctx context.Context,
	upload UploadPlanEntry,
	source FinalUploadFile,
	recoverable bool,
) error {
	report := func(bytes int64) { r.report(source, bytes, false) }
	if source.Size < r.rules.DirectUploadMaxLogicalBytes {
		data, compression, useParts, err := prepareUploadRoute(
			source,
			upload,
			r.rules.Compression,
			r.rules.MaxUploadBodyBytes,
		)
		if err != nil {
			return err
		}
		if !useParts {
			return r.drive.uploadPreparedDirect(ctx, upload, source, data, compression, recoverable, report)
		}
	}
	return r.drive.uploadParts(ctx, upload, source, r.rules, recoverable, r.partLimiter, report)
}
