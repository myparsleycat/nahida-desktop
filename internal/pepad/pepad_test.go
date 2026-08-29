package pepad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsesMinimalPE64DLL(t *testing.T) {
	report, err := Analyze(minimalDLL(retThenInt3Padding(32)), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !report.IsDLL || report.EntryPointRVA != textRVA || report.DiscoveredRegions < 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRejectsNonPEInput(t *testing.T) {
	_, err := Analyze([]byte("not a pe"), DefaultOptions())
	if !IsInvalidPE(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsTruncatedPEInput(t *testing.T) {
	_, err := Analyze([]byte{0x4d, 0x5a}, DefaultOptions())
	if !IsInvalidPE(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejects32BitPE(t *testing.T) {
	_, err := Analyze(pe32Fixture(retThenInt3Padding(32)), DefaultOptions())
	if !IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsEXEWhenDLLOnly(t *testing.T) {
	_, err := Analyze(exeFixture(retThenInt3Padding(32)), DefaultOptions())
	if !IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsOverlappingSections(t *testing.T) {
	_, err := Analyze(overlappingSectionsFixture(retThenInt3Padding(32)), DefaultOptions())
	if !IsInvalidPE(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsSignedTransformByDefault(t *testing.T) {
	_, err := Transform(signedFixture(retThenInt3Padding(32)), DefaultOptions())
	if !IsAuthenticode(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindsInt3PaddingAfterRet(t *testing.T) {
	report, err := Analyze(minimalDLL(retThenInt3Padding(24)), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range report.Candidates {
		if candidate.Status == StatusApproved {
			return
		}
	}
	t.Fatalf("no approved candidate: %#v", report.Candidates)
}

func TestRejectsNOPBytesEmbeddedInsideInstruction(t *testing.T) {
	text := []byte{0xb8, 0x90, 0x90, 0x90, 0x90, 0xc3}
	for len(text) < 0x200 {
		text = append(text, 0xcc)
	}
	opts := DefaultOptions()
	opts.MinimumSledLength = 4
	report, err := Analyze(minimalDLL(text), opts)
	if err != nil {
		t.Fatal(err)
	}
	var embedded *Candidate
	for i := range report.Candidates {
		if report.Candidates[i].RVA == textRVA+1 {
			embedded = &report.Candidates[i]
			break
		}
	}
	if embedded == nil || embedded.Status != StatusRejected {
		t.Fatalf("embedded = %#v", embedded)
	}
	if !containsReason(embedded.RejectionReasons, "decoded reachable instruction") {
		t.Fatalf("reasons = %#v", embedded.RejectionReasons)
	}
}

func TestZeroPaddingRequiresExplicitAlignmentTailProof(t *testing.T) {
	text := []byte{0xc3}
	for range 16 {
		text = append(text, 0)
	}
	for len(text) < 0x200 {
		text = append(text, 0xcc)
	}
	report, err := Analyze(minimalDLL(text), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range report.Candidates {
		if candidate.ByteKind == string(paddingZero) {
			t.Fatalf("unexpected zero candidate: %#v", candidate)
		}
	}
}

func TestNoSafeCandidateProducesIdenticalOutput(t *testing.T) {
	input := minimalDLL(noPaddingText())
	result, err := Transform(input, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Output, input) || result.Report.ModifiedRegions != 0 {
		t.Fatalf("result = %#v", result.Report)
	}
}

func TestRejectsDirectBranchTargetEnteringCandidate(t *testing.T) {
	text := []byte{0xe9, 0x0b, 0x00, 0x00, 0x00}
	for range 11 {
		text = append(text, 0x90)
	}
	for range 32 {
		text = append(text, 0xcc)
	}
	for len(text) < 0x200 {
		text = append(text, 0xcc)
	}
	report, err := Analyze(minimalDLL(text), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	var target *Candidate
	for i := range report.Candidates {
		if report.Candidates[i].RVA == textRVA+0x10 {
			target = &report.Candidates[i]
			break
		}
	}
	if target == nil || target.Status != StatusRejected || !containsReason(target.RejectionReasons, "direct branch") {
		t.Fatalf("target = %#v", target)
	}
}

func TestRejectsExceptionRuntimeFunctionOverlap(t *testing.T) {
	report, err := Analyze(exceptionFixture(retThenInt3Padding(32), textRVA, textRVA+0x40), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range report.Candidates {
		if candidate.Status == StatusRejected && containsReason(candidate.RejectionReasons, "runtime-function") {
			return
		}
	}
	t.Fatalf("candidates = %#v", report.Candidates)
}

func TestRejectsRelocationTargetOverlap(t *testing.T) {
	report, err := Analyze(relocationFixture(retThenInt3Padding(32), textRVA+8), DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range report.Candidates {
		if candidate.Status == StatusRejected && containsReason(candidate.RejectionReasons, "relocation target") {
			return
		}
	}
	t.Fatalf("candidates = %#v", report.Candidates)
}

func TestTransformationPreservesLayoutAndOnlyChangesPlannedRanges(t *testing.T) {
	opts := DefaultOptions()
	opts.Seed = 7
	input := minimalDLL(retThenInt3Padding(48))
	result, err := Transform(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) != len(input) || !result.Report.Validation.ReparsedOutput || !result.Report.Validation.SectionLayoutUnchanged || !result.Report.Validation.OnlyPlannedRangesChanged || result.Report.ModifiedRegions == 0 {
		t.Fatalf("report = %#v", result.Report)
	}
}

func TestFixedSeedIsByteForByteDeterministic(t *testing.T) {
	input := minimalDLL(retThenInt3Padding(64))
	opts := DefaultOptions()
	opts.Seed = 1234
	first, err := Transform(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Transform(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Output, second.Output) || !first.Report.Validation.Deterministic {
		t.Fatal("output was not deterministic")
	}
}

func TestDifferentSeedChangesOutputWhenCandidateExists(t *testing.T) {
	input := minimalDLL(retThenInt3Padding(64))
	firstOpts, secondOpts := DefaultOptions(), DefaultOptions()
	firstOpts.Seed, secondOpts.Seed = 1, 2
	first, err := Transform(input, firstOpts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Transform(input, secondOpts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Output, second.Output) {
		t.Fatal("different seeds produced identical output")
	}
}

func TestVerifyAcceptsTransformedOutput(t *testing.T) {
	input := minimalDLL(retThenInt3Padding(64))
	opts := DefaultOptions()
	opts.Seed = 99
	transformed, err := Transform(input, opts)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Verify(input, transformed.Output, DefaultOptions())
	if err != nil || !report.Validation.ReparsedOutput {
		t.Fatalf("report = %#v, %v", report, err)
	}
}

func TestInputBufferIsNotModified(t *testing.T) {
	input := minimalDLL(retThenInt3Padding(64))
	original := append([]byte(nil), input...)
	opts := DefaultOptions()
	opts.Seed = 42
	if _, err := Transform(input, opts); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, original) {
		t.Fatal("input buffer was modified")
	}
}

func TestDiversifyFileWritesOutput(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.dll")
	output := filepath.Join(root, "output.dll")
	if err := os.WriteFile(input, minimalDLL(retThenInt3Padding(64)), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := DiversifyFile(input, output)
	if err != nil {
		t.Fatal(err)
	}
	if report.DiscoveredRegions == 0 || report.ModifiedRegions == 0 || len(report.Patches) == 0 || report.OutputSHA256 == nil {
		t.Fatalf("report = %#v", report)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(got) != *report.OutputSHA256 || sha256Hex(got) == report.InputSHA256 {
		t.Fatalf("hash mismatch: %#v", report)
	}
}

func TestAllowInvalidSignatureTransformsSignedDLL(t *testing.T) {
	opts := DefaultOptions()
	opts.AllowInvalidSignature = true
	result, err := Transform(signedFixture(retThenInt3Padding(32)), opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.ModifiedRegions == 0 {
		t.Fatalf("report = %#v", result.Report)
	}
}

func containsReason(reasons []string, fragment string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, fragment) {
			return true
		}
	}
	return false
}

func TestErrorSentinels(t *testing.T) {
	if !errors.Is(invalidPE("x"), ErrInvalidPE) {
		t.Fatal("invalid PE sentinel")
	}
}
