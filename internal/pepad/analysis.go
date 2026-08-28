package pepad

import (
	"fmt"
	"slices"

	"golang.org/x/arch/x86/x86asm"
)

type paddingByteKind string

const (
	paddingNOP  paddingByteKind = "nop"
	paddingInt3 paddingByteKind = "int3"
	paddingZero paddingByteKind = "zero"
)

type paddingCandidate struct {
	ID         int
	Section    string
	RVA        uint32
	FileOffset int
	Length     int
	ByteKind   paddingByteKind
}

type approvedCandidate struct {
	ID         int
	Section    string
	RVA        uint32
	FileOffset int
	Length     int
}

type codeAnalysis struct {
	InstructionRanges []addressRange
	BranchTargets     []uint32
	ReferencedRVAs    []uint32
	Roots             []uint32
	DecodeErrors      []string
}

func collectCodeRoots(pe peImage) []uint32 {
	var roots []uint32
	if pe.Headers.EntryPoint != 0 && pe.isExecutableRVA(pe.Headers.EntryPoint) {
		roots = append(roots, pe.Headers.EntryPoint)
	}
	for _, section := range pe.executableSections() {
		roots = append(roots, section.VirtualAddress)
	}
	for _, rva := range pe.ExportRVAs {
		if pe.isExecutableRVA(rva) {
			roots = append(roots, rva)
		}
	}
	for _, function := range pe.RuntimeFunctions {
		if pe.isExecutableRVA(function.Begin) {
			roots = append(roots, function.Begin)
		}
	}
	roots = append(roots, pe.RelocationCodeRoots...)
	roots = append(roots, pe.TLSCallbackRVAs...)
	roots = append(roots, pe.GuardCFFunctionRVAs...)
	slices.Sort(roots)
	return slices.Compact(roots)
}

func analyzeCode(pe peImage, data []byte, roots []uint32) (codeAnalysis, error) {
	result := codeAnalysis{Roots: slices.Clone(roots)}
	queue := slices.Clone(roots)
	queued := map[uint32]struct{}{}
	for _, root := range roots {
		queued[root] = struct{}{}
	}
	decodedStarts := map[uint32]struct{}{}
	for len(queue) > 0 {
		root := queue[0]
		queue = queue[1:]
		if !pe.isExecutableRVA(root) {
			continue
		}
		var err error
		queue, err = decodeFromRoot(pe, data, root, decodedStarts, queued, queue, &result)
		if err != nil {
			return codeAnalysis{}, err
		}
	}
	slices.SortFunc(result.InstructionRanges, func(a, b addressRange) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		case a.Len < b.Len:
			return -1
		case a.Len > b.Len:
			return 1
		default:
			return 0
		}
	})
	slices.Sort(result.BranchTargets)
	result.BranchTargets = slices.Compact(result.BranchTargets)
	slices.Sort(result.ReferencedRVAs)
	result.ReferencedRVAs = slices.Compact(result.ReferencedRVAs)
	return result, nil
}

func decodeFromRoot(
	pe peImage,
	data []byte,
	root uint32,
	decodedStarts map[uint32]struct{},
	queued map[uint32]struct{},
	queue []uint32,
	result *codeAnalysis,
) ([]uint32, error) {
	for current := root; pe.isExecutableRVA(current); {
		if _, seen := decodedStarts[current]; seen {
			break
		}
		fileOffset, err := pe.rvaToFileOffset(current)
		if err != nil {
			result.DecodeErrors = append(result.DecodeErrors, fmt.Sprintf("decode root 0x%x: RVA 0x%x did not map to file data: %v", root, current, err))
			break
		}
		section, ok := pe.sectionByRVA(current)
		if !ok {
			break
		}
		rawEnd := int(section.rawEnd())
		if fileOffset >= rawEnd || fileOffset >= len(data) {
			break
		}
		if paddingStop(data, fileOffset, rawEnd) {
			break
		}
		limit := rawEnd
		if limit > len(data) {
			limit = len(data)
		}
		inst, err := x86asm.Decode(data[fileOffset:limit], 64)
		if err != nil || inst.Len == 0 {
			result.DecodeErrors = append(result.DecodeErrors, fmt.Sprintf("decode failed at RVA 0x%x from root 0x%x", current, root))
			break
		}
		decodedStarts[current] = struct{}{}
		result.InstructionRanges = append(result.InstructionRanges, newRange(current, uint32(inst.Len)))
		queue = collectReferences(pe, inst, current, result, queued, queue)
		next, err := addU32(current, uint32(inst.Len), "next instruction RVA")
		if err != nil {
			return queue, err
		}
		switch flowOf(inst) {
		case flowNext, flowCall, flowIndirectCall:
			current = next
		case flowConditional:
			if _, exists := queued[next]; !exists {
				queued[next] = struct{}{}
				queue = append(queue, next)
			}
			current = next
		default:
			return queue, nil
		}
	}
	return queue, nil
}

type flowKind int

const (
	flowNext flowKind = iota
	flowCall
	flowIndirectCall
	flowConditional
	flowStop
)

func flowOf(inst x86asm.Inst) flowKind {
	switch inst.Op {
	case x86asm.CALL, x86asm.LCALL, x86asm.SYSCALL, x86asm.SYSENTER:
		if isIndirectControl(inst) {
			return flowIndirectCall
		}
		return flowCall
	case x86asm.JMP, x86asm.LJMP:
		return flowStop
	case x86asm.RET, x86asm.LRET, x86asm.IRET, x86asm.IRETD, x86asm.IRETQ,
		x86asm.SYSRET, x86asm.SYSEXIT, x86asm.INT, x86asm.INTO, x86asm.UD2,
		x86asm.HLT, x86asm.XBEGIN:
		return flowStop
	case x86asm.JA, x86asm.JAE, x86asm.JB, x86asm.JBE, x86asm.JE, x86asm.JG,
		x86asm.JGE, x86asm.JL, x86asm.JLE, x86asm.JNE, x86asm.JNO, x86asm.JNP,
		x86asm.JNS, x86asm.JO, x86asm.JP, x86asm.JS, x86asm.JCXZ, x86asm.JECXZ,
		x86asm.JRCXZ, x86asm.LOOP, x86asm.LOOPE, x86asm.LOOPNE:
		return flowConditional
	default:
		if isIndirectControl(inst) && inst.Op == x86asm.JMP {
			return flowStop
		}
		return flowNext
	}
}

func isIndirectControl(inst x86asm.Inst) bool {
	if len(inst.Args) == 0 {
		return false
	}
	switch inst.Args[0].(type) {
	case x86asm.Mem, x86asm.Reg:
		return true
	default:
		return false
	}
}

func collectReferences(
	pe peImage,
	inst x86asm.Inst,
	current uint32,
	result *codeAnalysis,
	queued map[uint32]struct{},
	queue []uint32,
) []uint32 {
	if target, ok := nearBranchTarget(inst, current); ok {
		result.BranchTargets = append(result.BranchTargets, target)
		if pe.isExecutableRVA(target) {
			if _, exists := queued[target]; !exists {
				queued[target] = struct{}{}
				queue = append(queue, target)
			}
		}
	}
	for _, arg := range inst.Args {
		mem, ok := arg.(x86asm.Mem)
		if !ok || mem.Base != x86asm.RIP {
			continue
		}
		target := uint64(current) + uint64(inst.Len) + uint64(mem.Disp)
		if target <= uint64(^uint32(0)) {
			result.ReferencedRVAs = append(result.ReferencedRVAs, uint32(target))
		}
	}
	return queue
}

func nearBranchTarget(inst x86asm.Inst, current uint32) (uint32, bool) {
	switch inst.Op {
	case x86asm.CALL, x86asm.JMP, x86asm.JA, x86asm.JAE, x86asm.JB, x86asm.JBE,
		x86asm.JE, x86asm.JG, x86asm.JGE, x86asm.JL, x86asm.JLE, x86asm.JNE,
		x86asm.JNO, x86asm.JNP, x86asm.JNS, x86asm.JO, x86asm.JP, x86asm.JS,
		x86asm.LOOP, x86asm.LOOPE, x86asm.LOOPNE, x86asm.JCXZ, x86asm.JECXZ, x86asm.JRCXZ:
	default:
		return 0, false
	}
	if len(inst.Args) == 0 {
		return 0, false
	}
	rel, ok := inst.Args[0].(x86asm.Rel)
	if !ok {
		return 0, false
	}
	target := int64(current) + int64(inst.Len) + int64(rel)
	if target < 0 || target > int64(^uint32(0)) {
		return 0, false
	}
	return uint32(target), true
}

func paddingStop(data []byte, offset, rawEnd int) bool {
	limit := rawEnd
	if limit > len(data) {
		limit = len(data)
	}
	if offset >= limit {
		return true
	}
	b := data[offset]
	if b != 0x90 && b != 0xcc && b != 0x00 {
		return false
	}
	count := 0
	for _, current := range data[offset:limit] {
		if current == b {
			count++
			if count >= 8 {
				return true
			}
			continue
		}
		break
	}
	return false
}

func discoverPadding(pe peImage, data []byte, opts Options) ([]paddingCandidate, error) {
	minimum := opts.MinimumSledLength
	if minimum < 1 {
		minimum = 1
	}
	var candidates []paddingCandidate
	for _, section := range pe.executableSections() {
		start := int(section.PointerToRawData)
		end, err := addUsize(start, int(section.SizeOfRawData), "section raw")
		if err != nil {
			return nil, err
		}
		if end > len(data) {
			continue
		}
		cursor := start
		for cursor < end {
			kind, ok := paddingKind(data[cursor], opts.AllowZeroPadding)
			if !ok {
				cursor++
				continue
			}
			runEnd := cursor + 1
			for runEnd < end && data[runEnd] == data[cursor] {
				runEnd++
			}
			length := runEnd - cursor
			if length >= minimum {
				delta := cursor - start
				candidates = append(candidates, paddingCandidate{
					ID: len(candidates), Section: section.Name,
					RVA: section.VirtualAddress + uint32(delta), FileOffset: cursor,
					Length: length, ByteKind: kind,
				})
			}
			cursor = runEnd
		}
	}
	return candidates, nil
}

func paddingKind(b byte, allowZero bool) (paddingByteKind, bool) {
	switch b {
	case 0x90:
		return paddingNOP, true
	case 0xcc:
		return paddingInt3, true
	case 0x00:
		if allowZero {
			return paddingZero, true
		}
	}
	return "", false
}

func evaluateCandidates(pe peImage, code codeAnalysis, candidates []paddingCandidate, opts Options, report *Report) ([]approvedCandidate, error) {
	var approved []approvedCandidate
	relocationRanges := make([]addressRange, 0, len(pe.Relocations))
	for _, reloc := range pe.Relocations {
		relocationRanges = append(relocationRanges, reloc.RVARange)
	}
	runtimeRanges := make([]addressRange, 0, len(pe.RuntimeFunctions))
	for _, function := range pe.RuntimeFunctions {
		runtimeRanges = append(runtimeRanges, newRange(function.Begin, function.End-function.Begin))
	}
	for _, candidate := range candidates {
		var reasons []string
		rvaLen, err := uint32Checked(candidate.Length, "candidate length")
		if err != nil {
			return nil, err
		}
		rvaRange := newRange(candidate.RVA, rvaLen)
		fileRange, err := rangeFromUsize(candidate.FileOffset, candidate.Length, "candidate")
		if err != nil {
			return nil, err
		}
		if section, ok := pe.sectionByRVA(candidate.RVA); !ok || !section.executable() {
			reasons = append(reasons, "section is not executable")
		}
		switch mapped, mapErr := pe.rvaRangeToFileRange(candidate.RVA, rvaLen); {
		case mapErr != nil:
			reasons = append(reasons, fmt.Sprintf("ambiguous candidate RVA conversion: %v", mapErr))
		case mapped != fileRange:
			reasons = append(reasons, "candidate RVA and file range do not map exactly")
		}
		for _, inst := range code.InstructionRanges {
			if inst.overlaps(rvaRange) {
				reasons = append(reasons, "overlaps a decoded reachable instruction")
				break
			}
		}
		if rangeContainsAny(rvaRange, code.BranchTargets) {
			reasons = append(reasons, "a direct branch targets the candidate or its interior")
		}
		if pe.Headers.EntryPoint != 0 && rvaRange.contains(pe.Headers.EntryPoint) {
			reasons = append(reasons, "overlaps the PE entry point")
		}
		if rangeContainsAny(rvaRange, pe.ExportRVAs) {
			reasons = append(reasons, "overlaps an exported symbol RVA")
		}
		if rangeOverlapsAny(rvaRange, relocationRanges) {
			reasons = append(reasons, "overlaps a relocation target")
		}
		if rangeOverlapsAny(rvaRange, runtimeRanges) {
			reasons = append(reasons, "overlaps a runtime-function range")
		}
		for _, protected := range pe.ProtectedRanges {
			if (protected.RVARange != nil && protected.RVARange.overlaps(rvaRange)) ||
				(protected.FileRange != nil && protected.FileRange.overlaps(fileRange)) {
				reasons = append(reasons, "overlaps protected "+protected.Reason)
			}
		}
		if rangeContainsAny(rvaRange, code.ReferencedRVAs) {
			reasons = append(reasons, "a RIP-relative or data reference points at the candidate")
		}
		if candidate.ByteKind == paddingZero {
			if !opts.AllowZeroPadding {
				reasons = append(reasons, "zero padding is disabled")
			} else {
				ok, err := zeroPaddingIsAlignmentTail(pe, candidate)
				if err != nil {
					return nil, err
				}
				if !ok {
					reasons = append(reasons, "zero padding is not proven to be alignment tail padding")
				}
			}
		}
		status := StatusRejected
		if len(reasons) == 0 {
			approved = append(approved, approvedCandidate{
				ID: candidate.ID, Section: candidate.Section, RVA: candidate.RVA,
				FileOffset: candidate.FileOffset, Length: candidate.Length,
			})
			status = StatusApproved
		}
		report.addCandidate(Candidate{
			ID: candidate.ID, Section: candidate.Section, RVA: candidate.RVA,
			FileOffset: candidate.FileOffset, Length: candidate.Length,
			ByteKind: string(candidate.ByteKind), Status: status, RejectionReasons: reasons,
		})
	}
	return approved, nil
}

func zeroPaddingIsAlignmentTail(pe peImage, candidate paddingCandidate) (bool, error) {
	section, ok := pe.sectionByRVA(candidate.RVA)
	if !ok {
		return false, nil
	}
	candidateEnd, err := addU32(candidate.RVA, uint32(candidate.Length), "zero candidate end")
	if err != nil {
		return false, err
	}
	virtualEnd, err := addU32(section.VirtualAddress, section.VirtualSize, "section virtual end")
	if err != nil {
		return false, err
	}
	return candidate.RVA >= virtualEnd || candidateEnd > virtualEnd, nil
}

func uint32Checked(value int, context string) (uint32, error) {
	if value < 0 || value > int(^uint32(0)) {
		return 0, overflow(context)
	}
	return uint32(value), nil
}
