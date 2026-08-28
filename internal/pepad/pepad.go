package pepad

import (
	"bytes"
	"errors"
)

func Analyze(input []byte, opts Options) (Report, error) {
	analysis, err := analyzeInternal(input, normalize(opts))
	if err != nil {
		return Report{}, err
	}
	return analysis.report, nil
}

func Transform(input []byte, opts Options) (Result, error) {
	opts = normalize(opts)
	analysis, err := analyzeInternal(input, opts)
	if err != nil {
		return Result{}, err
	}
	if analysis.pe.hasCertificate() && !opts.AllowInvalidSignature && !opts.DryRun {
		return Result{}, ErrAuthenticode
	}
	patches, err := planPatches(analysis.approved, input, opts)
	if err != nil {
		return Result{}, err
	}
	repeated, err := planPatches(analysis.approved, input, opts)
	if err != nil {
		return Result{}, err
	}
	deterministic := patchesEqual(patches, repeated)
	if !deterministic {
		return Result{}, ErrNotDeterministic
	}
	analysis.report.markPatches(patches, opts.DryRun)
	var output []byte
	if !opts.DryRun {
		output, err = applyPatches(input, patches)
		if err != nil {
			return Result{}, err
		}
	} else {
		output = append([]byte(nil), input...)
	}
	if opts.DryRun {
		analysis.report.OutputSHA256 = nil
		analysis.report.Validation.Deterministic = deterministic
		return Result{Output: output, Report: analysis.report}, nil
	}
	validation, err := validateTransformation(input, output, analysis.pe, patches)
	if err != nil {
		return Result{}, err
	}
	validation.Deterministic = deterministic
	analysis.report.Validation = validation
	hash := sha256Hex(output)
	analysis.report.OutputSHA256 = &hash
	return Result{Output: output, Report: analysis.report}, nil
}

func Verify(original, output []byte, opts Options) (Report, error) {
	opts = normalize(opts)
	analysis, err := analyzeInternal(original, opts)
	if err != nil {
		return Report{}, err
	}
	patches, err := inferPatchesFromDiff(original, output, analysis.approved)
	if err != nil {
		return Report{}, err
	}
	validation, err := validateTransformation(original, output, analysis.pe, patches)
	if err != nil {
		return Report{}, err
	}
	analysis.report.markPatches(patches, false)
	hash := sha256Hex(output)
	analysis.report.OutputSHA256 = &hash
	analysis.report.Validation = validation
	return analysis.report, nil
}

type internalAnalysis struct {
	pe       peImage
	report   Report
	approved []approvedCandidate
}

func analyzeInternal(input []byte, opts Options) (internalAnalysis, error) {
	pe, err := parsePE(input)
	if err != nil {
		return internalAnalysis{}, err
	}
	if opts.DLLOnly && !pe.Headers.IsDLL {
		return internalAnalysis{}, unsupported("input is a PE image but is not marked as a DLL")
	}
	report := newReport(input, pe, opts)
	if pe.hasCertificate() {
		report.Warnings = append(report.Warnings, "Authenticode certificate table is present; byte changes invalidate the signature")
	}
	report.Warnings = append(report.Warnings, pe.Warnings...)
	roots := collectCodeRoots(pe)
	code, err := analyzeCode(pe, input, roots)
	if err != nil {
		return internalAnalysis{}, err
	}
	report.DecodeErrors = code.DecodeErrors
	candidates, err := discoverPadding(pe, input, opts)
	if err != nil {
		return internalAnalysis{}, err
	}
	approved, err := evaluateCandidates(pe, code, candidates, opts, &report)
	if err != nil {
		return internalAnalysis{}, err
	}
	report.recount()
	return internalAnalysis{pe: pe, report: report, approved: approved}, nil
}

func normalize(opts Options) Options {
	if opts == (Options{}) {
		return DefaultOptions()
	}
	if opts.MinimumSledLength == 0 {
		opts.MinimumSledLength = 8
	}
	return opts
}

func patchesEqual(a, b []patch) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].CandidateID != b[i].CandidateID || a[i].RVA != b[i].RVA || a[i].FileOffset != b[i].FileOffset || a[i].Template != b[i].Template {
			return false
		}
		if !bytes.Equal(a[i].Replacement, b[i].Replacement) {
			return false
		}
	}
	return true
}

func IsInvalidPE(err error) bool    { return errors.Is(err, ErrInvalidPE) }
func IsUnsupported(err error) bool  { return errors.Is(err, ErrUnsupported) }
func IsAuthenticode(err error) bool { return errors.Is(err, ErrAuthenticode) }
