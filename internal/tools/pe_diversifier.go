package tools

import (
	"context"
	"fmt"

	"nahida.live/desktop/internal/pepad"
)

func (t *Tools) runPEDiversifier(ctx context.Context, input, output string) (PEDiversificationReport, error) {
	if err := ctx.Err(); err != nil {
		return PEDiversificationReport{}, err
	}
	if t.peDiversifier != nil {
		return t.peDiversifier.Diversify(ctx, input, output)
	}
	report, err := pepad.DiversifyFile(input, output)
	if err != nil {
		return PEDiversificationReport{}, fmt.Errorf("PE padding diversifier failed: %w", err)
	}
	return toPEDiversificationReport(report), nil
}

func toPEDiversificationReport(report pepad.Report) PEDiversificationReport {
	out := PEDiversificationReport{
		DiscoveredRegions: report.DiscoveredRegions,
		ModifiedRegions:   report.ModifiedRegions,
		InputSHA256:       report.InputSHA256,
		OutputSHA256:      report.OutputSHA256,
	}
	for _, patch := range report.Patches {
		out.Patches = append(out.Patches, PEDiversifierPatch{CandidateID: patch.CandidateID})
	}
	return out
}
