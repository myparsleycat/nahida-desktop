package pepad

import (
	"fmt"
	"reflect"
)

func validateTransformation(original, output []byte, originalPE peImage, patches []patch) (Validation, error) {
	outputPE, err := parsePE(output)
	if err != nil {
		return Validation{}, err
	}
	summary := Validation{
		ReparsedOutput:           true,
		FileLengthUnchanged:      len(original) == len(output),
		SectionLayoutUnchanged:   reflect.DeepEqual(originalPE.Sections, outputPE.Sections),
		EntryPointUnchanged:      originalPE.Headers.EntryPoint == outputPE.Headers.EntryPoint,
		DataDirectoriesUnchanged: reflect.DeepEqual(originalPE.Headers.DataDirectories, outputPE.Headers.DataDirectories),
		RelocationsUnchanged:     reflect.DeepEqual(originalPE.Relocations, outputPE.Relocations),
		Deterministic:            true,
	}
	if !summary.FileLengthUnchanged {
		return Validation{}, validationErr("file length changed during transformation")
	}
	if !summary.SectionLayoutUnchanged {
		return Validation{}, validationErr("section layout changed during transformation")
	}
	if originalPE.Headers.NumberOfSections != outputPE.Headers.NumberOfSections {
		return Validation{}, validationErr("section count changed during transformation")
	}
	if !summary.EntryPointUnchanged {
		return Validation{}, validationErr("entry point changed during transformation")
	}
	if originalPE.Headers.SizeOfImage != outputPE.Headers.SizeOfImage {
		return Validation{}, validationErr("SizeOfImage changed during transformation")
	}
	if !summary.DataDirectoriesUnchanged {
		return Validation{}, validationErr("data-directory RVAs or sizes changed during transformation")
	}
	equal, err := directoriesEqual(original, output, originalPE, []int{
		importDirectory, iatDirectory, delayImportDirectory, exportDirectory,
		resourceDirectory, loadConfigDirectory, tlsDirectory,
	})
	if err != nil {
		return Validation{}, err
	}
	summary.ImportsExportsMetadataUnchanged = equal
	if !equal {
		return Validation{}, validationErr("import/export/resource/TLS/load-config metadata bytes changed")
	}
	equal, err = directoriesEqual(original, output, originalPE, []int{exceptionDirectory})
	if err != nil {
		return Validation{}, err
	}
	summary.ExceptionMetadataUnchanged = equal
	if !equal || !reflect.DeepEqual(originalPE.RuntimeFunctions, outputPE.RuntimeFunctions) {
		return Validation{}, validationErr("exception directory or runtime-function entries changed")
	}
	relocBytesEqual, err := directoriesEqual(original, output, originalPE, []int{baserelocDirectory})
	if err != nil {
		return Validation{}, err
	}
	summary.RelocationsUnchanged = relocBytesEqual && reflect.DeepEqual(originalPE.Relocations, outputPE.Relocations)
	if !summary.RelocationsUnchanged {
		return Validation{}, validationErr("relocation entries changed")
	}
	changed := changedRanges(original, output)
	summary.OnlyPlannedRangesChanged = true
	for _, rng := range changed {
		if !anyPatchCovers(patches, rng) {
			summary.OnlyPlannedRangesChanged = false
			break
		}
	}
	if !summary.OnlyPlannedRangesChanged {
		return Validation{}, validationErr("bytes changed outside the patch plan")
	}
	for _, item := range patches {
		end, err := addUsize(item.FileOffset, len(item.Replacement), "patch validation range")
		if err != nil {
			return Validation{}, err
		}
		if end > len(output) || string(output[item.FileOffset:end]) != string(item.Replacement) {
			return Validation{}, validationErr(fmt.Sprintf("patch bytes for candidate %d do not match the plan", item.CandidateID))
		}
	}
	summary.ChangedRangesInsideCandidates = true
	return summary, nil
}

func inferPatchesFromDiff(original, output []byte, approved []approvedCandidate) ([]patch, error) {
	if len(original) != len(output) {
		return nil, validationErr("files have different lengths")
	}
	var patches []patch
	for _, rng := range changedRanges(original, output) {
		var match *approvedCandidate
		for i := range approved {
			candidate := &approved[i]
			candidateRange := newRange(uint32(candidate.FileOffset), uint32(candidate.Length))
			if candidateRange.overlaps(rng) && candidateRange.Start <= rng.Start && rng.endU64() <= candidateRange.endU64() {
				match = candidate
				break
			}
		}
		if match == nil {
			return nil, validationErr(fmt.Sprintf("changed range 0x%x+0x%x is not inside an approved padding candidate", rng.Start, rng.Len))
		}
		start := int(rng.Start)
		end := int(rng.end())
		patches = append(patches, patch{
			CandidateID: match.ID,
			RVA:         match.RVA + uint32(start-match.FileOffset),
			FileOffset:  start,
			Replacement: append([]byte(nil), output[start:end]...),
			Template:    "external_diff",
		})
	}
	return patches, nil
}

func changedRanges(a, b []byte) []addressRange {
	if len(a) != len(b) {
		return nil
	}
	var ranges []addressRange
	for cursor := 0; cursor < len(a); {
		if a[cursor] == b[cursor] {
			cursor++
			continue
		}
		start := cursor
		for cursor < len(a) && a[cursor] != b[cursor] {
			cursor++
		}
		ranges = append(ranges, newRange(uint32(start), uint32(cursor-start)))
	}
	return ranges
}

func anyPatchCovers(patches []patch, rng addressRange) bool {
	for _, item := range patches {
		patchRange := newRange(uint32(item.FileOffset), uint32(len(item.Replacement)))
		if patchRange.Start <= rng.Start && rng.endU64() <= patchRange.endU64() {
			return true
		}
	}
	return false
}

func directoriesEqual(original, output []byte, pe peImage, indexes []int) (bool, error) {
	for _, index := range indexes {
		rng, ok := pe.directoryFileRange(index)
		if !ok {
			continue
		}
		start := int(rng.Start)
		end := int(rng.end())
		if start > len(original) || end > len(original) || start > len(output) || end > len(output) {
			return false, nil
		}
		if string(original[start:end]) != string(output[start:end]) {
			return false, nil
		}
	}
	return true, nil
}
