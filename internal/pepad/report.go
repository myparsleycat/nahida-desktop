package pepad

type CandidateStatus string

const (
	StatusApproved  CandidateStatus = "approved"
	StatusRejected  CandidateStatus = "rejected"
	StatusModified  CandidateStatus = "modified"
	StatusUnchanged CandidateStatus = "unchanged"
)

type Candidate struct {
	ID               int             `json:"id"`
	Section          string          `json:"section"`
	RVA              uint32          `json:"rva"`
	FileOffset       int             `json:"file_offset"`
	Length           int             `json:"length"`
	ByteKind         string          `json:"byte_kind"`
	Status           CandidateStatus `json:"status"`
	RejectionReasons []string        `json:"rejection_reasons"`
	Template         *string         `json:"template"`
}

type PatchRecord struct {
	CandidateID       int    `json:"candidate_id"`
	RVA               uint32 `json:"rva"`
	FileOffset        int    `json:"file_offset"`
	Length            int    `json:"length"`
	Template          string `json:"template"`
	ReplacementSHA256 string `json:"replacement_sha256"`
}

type Validation struct {
	ReparsedOutput                  bool `json:"reparsed_output"`
	FileLengthUnchanged             bool `json:"file_length_unchanged"`
	SectionLayoutUnchanged          bool `json:"section_layout_unchanged"`
	EntryPointUnchanged             bool `json:"entry_point_unchanged"`
	DataDirectoriesUnchanged        bool `json:"data_directories_unchanged"`
	ImportsExportsMetadataUnchanged bool `json:"imports_exports_metadata_unchanged"`
	ExceptionMetadataUnchanged      bool `json:"exception_metadata_unchanged"`
	RelocationsUnchanged            bool `json:"relocations_unchanged"`
	OnlyPlannedRangesChanged        bool `json:"only_planned_ranges_changed"`
	ChangedRangesInsideCandidates   bool `json:"changed_ranges_inside_candidates"`
	Deterministic                   bool `json:"deterministic"`
}

type Report struct {
	ProgramVersion             string        `json:"program_version"`
	InputSHA256                string        `json:"input_sha256"`
	OutputSHA256               *string       `json:"output_sha256"`
	FileLength                 int           `json:"file_length"`
	ImageBase                  uint64        `json:"image_base"`
	EntryPointRVA              uint32        `json:"entry_point_rva"`
	IsDLL                      bool          `json:"is_dll"`
	HasAuthenticodeCertificate bool          `json:"has_authenticode_certificate"`
	PEChecksumHeader           uint32        `json:"pe_checksum_header"`
	PEChecksumComputed         uint32        `json:"pe_checksum_computed"`
	Options                    Options       `json:"options"`
	DiscoveredRegions          int           `json:"discovered_regions"`
	RejectedRegions            int           `json:"rejected_regions"`
	ApprovedRegions            int           `json:"approved_regions"`
	ModifiedRegions            int           `json:"modified_regions"`
	Candidates                 []Candidate   `json:"candidates"`
	Patches                    []PatchRecord `json:"patches"`
	Warnings                   []string      `json:"warnings"`
	DecodeErrors               []string      `json:"decode_errors"`
	Validation                 Validation    `json:"validation"`
	ReferenceConcepts          []string      `json:"reference_concepts"`
}

type Result struct {
	Output []byte
	Report Report
}

func newReport(input []byte, pe peImage, opts Options) Report {
	return Report{
		ProgramVersion:             programVersion,
		InputSHA256:                sha256Hex(input),
		FileLength:                 len(input),
		ImageBase:                  pe.Headers.ImageBase,
		EntryPointRVA:              pe.Headers.EntryPoint,
		IsDLL:                      pe.Headers.IsDLL,
		HasAuthenticodeCertificate: pe.hasCertificate(),
		PEChecksumHeader:           pe.Headers.Checksum,
		PEChecksumComputed:         computePEChecksum(input, pe.Headers.ChecksumFileOffset),
		Options:                    opts,
		Candidates:                 []Candidate{},
		Patches:                    []PatchRecord{},
		Warnings:                   []string{},
		DecodeErrors:               []string{},
		ReferenceConcepts: []string{
			"binprotect: first-class RVA tracking and conservative metadata awareness",
			"binprotect: branch/RIP-relative reference discovery as safety input",
			"ObfuGuard: PE64 validation and instruction-boundary awareness",
			"ObfuGuard: deterministic replacement planning instead of time randomness",
		},
	}
}

func (r *Report) addCandidate(candidate Candidate) {
	r.Candidates = append(r.Candidates, candidate)
	r.recount()
}

func (r *Report) markPatches(patches []patch, dryRun bool) {
	r.Patches = make([]PatchRecord, 0, len(patches))
	for _, item := range patches {
		r.Patches = append(r.Patches, PatchRecord{
			CandidateID: item.CandidateID, RVA: item.RVA, FileOffset: item.FileOffset,
			Length: len(item.Replacement), Template: item.Template,
			ReplacementSHA256: sha256Hex(item.Replacement),
		})
		for i := range r.Candidates {
			if r.Candidates[i].ID != item.CandidateID {
				continue
			}
			if dryRun {
				r.Candidates[i].Status = StatusApproved
			} else {
				r.Candidates[i].Status = StatusModified
			}
			template := item.Template
			r.Candidates[i].Template = &template
		}
	}
	r.recount()
}

func (r *Report) recount() {
	r.DiscoveredRegions = len(r.Candidates)
	r.RejectedRegions = 0
	r.ApprovedRegions = 0
	r.ModifiedRegions = 0
	for _, candidate := range r.Candidates {
		switch candidate.Status {
		case StatusRejected:
			r.RejectedRegions++
		case StatusApproved, StatusModified, StatusUnchanged:
			r.ApprovedRegions++
		}
		if candidate.Status == StatusModified {
			r.ModifiedRegions++
		}
	}
}
