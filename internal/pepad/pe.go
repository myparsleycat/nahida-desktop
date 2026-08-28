package pepad

import (
	"encoding/binary"
	"fmt"
	"slices"
)

const (
	exportDirectory = iota
	importDirectory
	resourceDirectory
	exceptionDirectory
	securityDirectory
	baserelocDirectory
	debugDirectory
	architectureDirectory
	globalptrDirectory
	tlsDirectory
	loadConfigDirectory
	boundImportDirectory
	iatDirectory
	delayImportDirectory
	clrDirectory
)

const (
	dosMagic           = 0x5a4d
	peSignature        = 0x00004550
	machineAMD64       = 0x8664
	optionalMagicPE32  = 0x10b
	optionalMagicPE32P = 0x20b
	imageFileDLL       = 0x2000
	imageSCNMemExecute = 0x20000000
	imageRelBasedAbs   = 0
	imageRelBasedHigh  = 3
	imageRelBasedDir64 = 10
)

type dataDirectory struct {
	VirtualAddress uint32 `json:"virtual_address"`
	Size           uint32 `json:"size"`
}

func (d dataDirectory) present() bool {
	return d.VirtualAddress != 0 && d.Size != 0
}

func directoryName(index int) string {
	switch index {
	case exportDirectory:
		return "export"
	case importDirectory:
		return "import"
	case resourceDirectory:
		return "resource"
	case exceptionDirectory:
		return "exception"
	case securityDirectory:
		return "security/certificate"
	case baserelocDirectory:
		return "base relocation"
	case debugDirectory:
		return "debug"
	case architectureDirectory:
		return "architecture"
	case globalptrDirectory:
		return "global pointer"
	case tlsDirectory:
		return "TLS"
	case loadConfigDirectory:
		return "load-config"
	case boundImportDirectory:
		return "bound import"
	case iatDirectory:
		return "IAT"
	case delayImportDirectory:
		return "delay import"
	case clrDirectory:
		return "CLR"
	default:
		return "reserved"
	}
}

type peHeaders struct {
	NTOffset             int
	COFFOffset           int
	OptionalOffset       int
	SectionTableOffset   int
	Machine              uint16
	NumberOfSections     uint16
	SizeOfOptionalHeader uint16
	Characteristics      uint16
	EntryPoint           uint32
	ImageBase            uint64
	SectionAlignment     uint32
	FileAlignment        uint32
	SizeOfImage          uint32
	SizeOfHeaders        uint32
	Checksum             uint32
	ChecksumFileOffset   int
	Subsystem            uint16
	DLLCharacteristics   uint16
	NumberOfRvaAndSizes  uint32
	DataDirectories      []dataDirectory
	IsDLL                bool
}

type sectionHeader struct {
	Name             string
	VirtualSize      uint32
	VirtualAddress   uint32
	SizeOfRawData    uint32
	PointerToRawData uint32
	Characteristics  uint32
}

func (s sectionHeader) rawEnd() uint32 {
	return s.PointerToRawData + s.SizeOfRawData
}

func (s sectionHeader) virtualSpan() uint32 {
	if s.VirtualSize > s.SizeOfRawData {
		return s.VirtualSize
	}
	return s.SizeOfRawData
}

func (s sectionHeader) containsRVA(rva uint32) bool {
	start := uint64(s.VirtualAddress)
	end := start + uint64(s.virtualSpan())
	return uint64(rva) >= start && uint64(rva) < end
}

func (s sectionHeader) rvaToFileOffset(rva uint32) (int, bool) {
	if !s.containsRVA(rva) {
		return 0, false
	}
	if rva < s.VirtualAddress {
		return 0, false
	}
	delta := rva - s.VirtualAddress
	if delta >= s.SizeOfRawData {
		return 0, false
	}
	return int(s.PointerToRawData) + int(delta), true
}

func (s sectionHeader) executable() bool {
	return s.Characteristics&imageSCNMemExecute != 0
}

type protectedRange struct {
	RVARange  *addressRange
	FileRange *addressRange
	Reason    string
}

type relocationTarget struct {
	RVARange       addressRange
	RelocationType uint16
}

type runtimeFunction struct {
	Begin         uint32
	End           uint32
	UnwindInfoRVA uint32
}

type peImage struct {
	Data                []byte
	Headers             peHeaders
	Sections            []sectionHeader
	ProtectedRanges     []protectedRange
	Relocations         []relocationTarget
	RuntimeFunctions    []runtimeFunction
	ExportRVAs          []uint32
	RelocationCodeRoots []uint32
	TLSCallbackRVAs     []uint32
	GuardCFFunctionRVAs []uint32
	Warnings            []string
}

func parsePE(data []byte) (peImage, error) {
	headers, err := parseHeaders(data)
	if err != nil {
		return peImage{}, err
	}
	sections, err := parseSections(data, headers)
	if err != nil {
		return peImage{}, err
	}
	if err := validateSections(data, sections); err != nil {
		return peImage{}, err
	}
	image := peImage{Data: data, Headers: headers, Sections: sections}
	if err := image.extractMetadata(); err != nil {
		return peImage{}, err
	}
	return image, nil
}

func (p peImage) hasCertificate() bool {
	if securityDirectory >= len(p.Headers.DataDirectories) {
		return false
	}
	return p.Headers.DataDirectories[securityDirectory].present()
}

func (p peImage) executableSections() []sectionHeader {
	var out []sectionHeader
	for _, section := range p.Sections {
		if section.executable() {
			out = append(out, section)
		}
	}
	return out
}

func (p peImage) sectionByRVA(rva uint32) (sectionHeader, bool) {
	for _, section := range p.Sections {
		if section.containsRVA(rva) {
			return section, true
		}
	}
	return sectionHeader{}, false
}

func (p peImage) isExecutableRVA(rva uint32) bool {
	section, ok := p.sectionByRVA(rva)
	return ok && section.executable()
}

func (p peImage) rvaToFileOffset(rva uint32) (int, error) {
	if rva < p.Headers.SizeOfHeaders {
		offset := int(rva)
		if offset < len(p.Data) {
			return offset, nil
		}
	}
	var first *int
	for _, section := range p.Sections {
		offset, ok := section.rvaToFileOffset(rva)
		if !ok {
			continue
		}
		if first != nil {
			return 0, addressErr(fmt.Sprintf("RVA 0x%x maps to more than one section", rva))
		}
		value := offset
		first = &value
	}
	if first == nil {
		return 0, addressErr(fmt.Sprintf("RVA 0x%x does not map to file data", rva))
	}
	return *first, nil
}

func (p peImage) rvaRangeToFileRange(rva, length uint32) (addressRange, error) {
	if length == 0 {
		offset, err := p.rvaToFileOffset(rva)
		if err != nil {
			return addressRange{}, err
		}
		return newRange(uint32(offset), 0), nil
	}
	endMinusOne, err := addU32(rva, length-1, "RVA range end")
	if err != nil {
		return addressRange{}, err
	}
	first, err := p.rvaToFileOffset(rva)
	if err != nil {
		return addressRange{}, err
	}
	last, err := p.rvaToFileOffset(endMinusOne)
	if err != nil {
		return addressRange{}, err
	}
	expectedLast, err := addUsize(first, int(length-1), "file range end")
	if err != nil {
		return addressRange{}, err
	}
	if last != expectedLast {
		return addressRange{}, addressErr(fmt.Sprintf("RVA range 0x%x+0x%x is not contiguous in the file", rva, length))
	}
	return rangeFromUsize(first, int(length), "file range")
}

func (p peImage) directoryFileRange(index int) (addressRange, bool) {
	if index >= len(p.Headers.DataDirectories) {
		return addressRange{}, false
	}
	directory := p.Headers.DataDirectories[index]
	if !directory.present() {
		return addressRange{}, false
	}
	if index == securityDirectory {
		rng, err := rangeFromUsize(int(directory.VirtualAddress), int(directory.Size), "certificate directory")
		if err != nil {
			return addressRange{}, false
		}
		return rng, true
	}
	rng, err := p.rvaRangeToFileRange(directory.VirtualAddress, directory.Size)
	if err != nil {
		return addressRange{}, false
	}
	return rng, true
}

func (p *peImage) extractMetadata() error {
	p.protectDataDirectories()
	if err := p.parseExports(); err != nil {
		return err
	}
	if err := p.parseRelocations(); err != nil {
		return err
	}
	if err := p.parseExceptions(); err != nil {
		return err
	}
	if err := p.parseTLS(); err != nil {
		return err
	}
	return p.parseLoadConfig()
}

func (p *peImage) protectDataDirectories() {
	directories := slices.Clone(p.Headers.DataDirectories)
	for index, directory := range directories {
		if !directory.present() {
			continue
		}
		reason := directoryName(index) + " data directory"
		if index == securityDirectory {
			fileRange, err := rangeFromUsize(int(directory.VirtualAddress), int(directory.Size), "certificate table")
			if err != nil {
				p.Warnings = append(p.Warnings, err.Error())
				continue
			}
			p.ProtectedRanges = append(p.ProtectedRanges, protectedRange{FileRange: &fileRange, Reason: reason})
			continue
		}
		rvaRange := newRange(directory.VirtualAddress, directory.Size)
		fileRange, err := p.rvaRangeToFileRange(directory.VirtualAddress, directory.Size)
		var filePtr *addressRange
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s at RVA 0x%x+0x%x does not map to one contiguous file range", directoryName(index), directory.VirtualAddress, directory.Size))
		} else {
			filePtr = &fileRange
		}
		p.ProtectedRanges = append(p.ProtectedRanges, protectedRange{RVARange: &rvaRange, FileRange: filePtr, Reason: reason})
	}
}

func (p *peImage) parseExports() error {
	rng, ok := p.directoryFileRange(exportDirectory)
	if !ok || rng.Len < 40 {
		return nil
	}
	base := int(rng.Start)
	numberOfFunctions, err := readU32(p.Data, base+20)
	if err != nil {
		return err
	}
	addressOfFunctions, err := readU32(p.Data, base+28)
	if err != nil {
		return err
	}
	functionsOffset, err := p.rvaToFileOffset(addressOfFunctions)
	if err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("export address table skipped: %v", err))
		return nil
	}
	exportDir := p.Headers.DataDirectories[exportDirectory]
	exportRange := newRange(exportDir.VirtualAddress, exportDir.Size)
	for index := range int(numberOfFunctions) {
		entryOffset, err := addUsize(functionsOffset, index*4, "export table")
		if err != nil {
			return err
		}
		if entryOffset+4 > len(p.Data) {
			break
		}
		functionRVA, err := readU32(p.Data, entryOffset)
		if err != nil {
			return err
		}
		if functionRVA == 0 || exportRange.contains(functionRVA) {
			continue
		}
		p.ExportRVAs = append(p.ExportRVAs, functionRVA)
	}
	slices.Sort(p.ExportRVAs)
	p.ExportRVAs = slices.Compact(p.ExportRVAs)
	return nil
}

func (p *peImage) parseRelocations() error {
	rng, ok := p.directoryFileRange(baserelocDirectory)
	if !ok {
		return nil
	}
	offset := int(rng.Start)
	end := int(rng.end())
	for offset+8 <= end {
		pageRVA, err := readU32(p.Data, offset)
		if err != nil {
			return err
		}
		blockSize32, err := readU32(p.Data, offset+4)
		if err != nil {
			return err
		}
		blockSize := int(blockSize32)
		if pageRVA == 0 || blockSize == 0 {
			break
		}
		if blockSize < 8 || offset+blockSize > end {
			p.Warnings = append(p.Warnings, "base relocation block has invalid size; remaining blocks skipped")
			break
		}
		entryCount := (blockSize - 8) / 2
		entriesOffset := offset + 8
		for index := range entryCount {
			raw, err := readU16(p.Data, entriesOffset+index*2)
			if err != nil {
				return err
			}
			relocationType := raw >> 12
			relocationOffset := raw & 0x0fff
			if relocationType == imageRelBasedAbs {
				continue
			}
			targetRVA, err := addU32(pageRVA, uint32(relocationOffset), "relocation target")
			if err != nil {
				return err
			}
			width := uint32(2)
			switch relocationType {
			case imageRelBasedDir64:
				width = 8
			case imageRelBasedHigh:
				width = 4
			}
			targetRange := newRange(targetRVA, width)
			p.Relocations = append(p.Relocations, relocationTarget{RVARange: targetRange, RelocationType: relocationType})
			p.addProtectedRVARange(targetRange, "relocation target")
			if relocationType == imageRelBasedDir64 {
				if fileOffset, err := p.rvaToFileOffset(targetRVA); err == nil && fileOffset+8 <= len(p.Data) {
					value, err := readU64(p.Data, fileOffset)
					if err != nil {
						return err
					}
					if value >= p.Headers.ImageBase {
						codeRVA := value - p.Headers.ImageBase
						if codeRVA <= uint64(^uint32(0)) && p.isExecutableRVA(uint32(codeRVA)) {
							p.RelocationCodeRoots = append(p.RelocationCodeRoots, uint32(codeRVA))
						}
					}
				}
			}
		}
		offset += blockSize
	}
	slices.Sort(p.RelocationCodeRoots)
	p.RelocationCodeRoots = slices.Compact(p.RelocationCodeRoots)
	return nil
}

func (p *peImage) parseExceptions() error {
	rng, ok := p.directoryFileRange(exceptionDirectory)
	if !ok {
		return nil
	}
	offset := int(rng.Start)
	end := int(rng.end())
	for offset+12 <= end {
		begin, err := readU32(p.Data, offset)
		if err != nil {
			return err
		}
		endRVA, err := readU32(p.Data, offset+4)
		if err != nil {
			return err
		}
		unwindRVA, err := readU32(p.Data, offset+8)
		if err != nil {
			return err
		}
		if begin != 0 && begin < endRVA {
			p.RuntimeFunctions = append(p.RuntimeFunctions, runtimeFunction{Begin: begin, End: endRVA, UnwindInfoRVA: unwindRVA})
		}
		p.protectUnwindInfo(unwindRVA)
		offset += 12
	}
	slices.SortFunc(p.RuntimeFunctions, func(a, b runtimeFunction) int {
		switch {
		case a.Begin < b.Begin:
			return -1
		case a.Begin > b.Begin:
			return 1
		default:
			return 0
		}
	})
	return nil
}

func (p *peImage) parseTLS() error {
	rng, ok := p.directoryFileRange(tlsDirectory)
	if !ok || rng.Len < 40 {
		return nil
	}
	base := int(rng.Start)
	startRawVA, err := readU64(p.Data, base)
	if err != nil {
		return err
	}
	endRawVA, err := readU64(p.Data, base+8)
	if err != nil {
		return err
	}
	callbacksVA, err := readU64(p.Data, base+24)
	if err != nil {
		return err
	}
	if start, ok := p.vaToRVA(startRawVA); ok {
		if end, ok := p.vaToRVA(endRawVA); ok && start < end {
			p.addProtectedRVARange(newRange(start, end-start), "TLS raw data")
		}
	}
	callbacksRVA, ok := p.vaToRVA(callbacksVA)
	if !ok {
		return nil
	}
	callbacksOffset, err := p.rvaToFileOffset(callbacksRVA)
	if err == nil {
		for index := range 1024 {
			entryOffset := callbacksOffset + index*8
			if entryOffset+8 > len(p.Data) {
				break
			}
			callbackVA, err := readU64(p.Data, entryOffset)
			if err != nil {
				return err
			}
			p.addProtectedRVARange(newRange(callbacksRVA+uint32(index)*8, 8), "TLS callback table")
			if callbackVA == 0 {
				break
			}
			if callbackRVA, ok := p.vaToRVA(callbackVA); ok && p.isExecutableRVA(callbackRVA) {
				p.TLSCallbackRVAs = append(p.TLSCallbackRVAs, callbackRVA)
			}
		}
		slices.Sort(p.TLSCallbackRVAs)
		p.TLSCallbackRVAs = slices.Compact(p.TLSCallbackRVAs)
	}
	return nil
}

func (p *peImage) parseLoadConfig() error {
	rng, ok := p.directoryFileRange(loadConfigDirectory)
	if !ok || rng.Len < 0x94 {
		return nil
	}
	base := int(rng.Start)
	loadConfigSize, err := readU32(p.Data, base)
	if err != nil {
		return err
	}
	if loadConfigSize < 0x94 {
		return nil
	}
	seHandlerTable, err := readU64(p.Data, base+0x60)
	if err != nil {
		return err
	}
	seHandlerCount, err := readU64(p.Data, base+0x68)
	if err != nil {
		return err
	}
	if err := p.protectVATable(seHandlerTable, seHandlerCount, 4, "load-config SE handler table", false); err != nil {
		return err
	}
	guardTable, err := readU64(p.Data, base+0x80)
	if err != nil {
		return err
	}
	guardCount, err := readU64(p.Data, base+0x88)
	if err != nil {
		return err
	}
	guardFlags, err := readU32(p.Data, base+0x90)
	if err != nil {
		return err
	}
	guardExtra := uint64((guardFlags >> 28) & 0x0f)
	return p.protectVATable(guardTable, guardCount, 4+guardExtra, "Guard CF function table", true)
}

func (p *peImage) protectVATable(tableVA, count, entrySize uint64, reason string, entriesAreCodeRVAs bool) error {
	if tableVA == 0 || count == 0 || entrySize == 0 {
		return nil
	}
	tableRVA, ok := p.vaToRVA(tableVA)
	if !ok {
		return nil
	}
	byteLen := count * entrySize
	if byteLen > uint64(^uint32(0)) {
		return overflow("load-config table size")
	}
	p.addProtectedRVARange(newRange(tableRVA, uint32(byteLen)), reason)
	if !entriesAreCodeRVAs {
		return nil
	}
	fileRange, err := p.rvaRangeToFileRange(tableRVA, uint32(byteLen))
	if err == nil {
		limit := count
		if limit > 100000 {
			limit = 100000
		}
		for index := range int(limit) {
			offset := int(fileRange.Start) + index*int(entrySize)
			if offset+4 > len(p.Data) {
				break
			}
			functionRVA, err := readU32(p.Data, offset)
			if err != nil {
				return err
			}
			if p.isExecutableRVA(functionRVA) {
				p.GuardCFFunctionRVAs = append(p.GuardCFFunctionRVAs, functionRVA)
			}
		}
		slices.Sort(p.GuardCFFunctionRVAs)
		p.GuardCFFunctionRVAs = slices.Compact(p.GuardCFFunctionRVAs)
	}
	return nil
}

func (p *peImage) protectUnwindInfo(unwindRVA uint32) {
	if unwindRVA == 0 {
		return
	}
	offset, err := p.rvaToFileOffset(unwindRVA)
	if err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("unwind info RVA 0x%x did not map to file data", unwindRVA))
		return
	}
	if offset+4 > len(p.Data) {
		return
	}
	flags := p.Data[offset] >> 3
	codeCount := int(p.Data[offset+2])
	codeBytes := codeCount * 2
	size := 4 + codeBytes
	if size%4 != 0 {
		size += 2
	}
	if flags&0x4 != 0 {
		size += 12
	} else if flags&0x3 != 0 {
		size += 4
	}
	if offset+size <= len(p.Data) {
		p.addProtectedRVARange(newRange(unwindRVA, uint32(size)), "unwind info")
	}
}

func (p *peImage) addProtectedRVARange(rvaRange addressRange, reason string) {
	fileRange, err := p.rvaRangeToFileRange(rvaRange.Start, rvaRange.Len)
	var filePtr *addressRange
	if err == nil {
		filePtr = &fileRange
	}
	p.ProtectedRanges = append(p.ProtectedRanges, protectedRange{RVARange: &rvaRange, FileRange: filePtr, Reason: reason})
}

func (p peImage) vaToRVA(va uint64) (uint32, bool) {
	if va < p.Headers.ImageBase {
		return 0, false
	}
	rva := va - p.Headers.ImageBase
	if rva > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(rva), true
}

func parseHeaders(data []byte) (peHeaders, error) {
	if len(data) < 0x40 {
		return peHeaders{}, invalidPE("file is too small for DOS header")
	}
	magic, err := readU16(data, 0)
	if err != nil {
		return peHeaders{}, err
	}
	if magic != dosMagic {
		return peHeaders{}, invalidPE("missing MZ header")
	}
	eLfanew, err := readI32(data, 0x3c)
	if err != nil {
		return peHeaders{}, err
	}
	if eLfanew < 0 {
		return peHeaders{}, invalidPE("negative e_lfanew is invalid")
	}
	ntOffset := int(eLfanew)
	sig, err := readU32(data, ntOffset)
	if err != nil {
		return peHeaders{}, err
	}
	if sig != peSignature {
		return peHeaders{}, invalidPE("missing PE signature")
	}
	coffOffset := ntOffset + 4
	machine, err := readU16(data, coffOffset)
	if err != nil {
		return peHeaders{}, err
	}
	if machine != machineAMD64 {
		return peHeaders{}, unsupported(fmt.Sprintf("machine 0x%04x is not AMD64", machine))
	}
	numberOfSections, err := readU16(data, coffOffset+2)
	if err != nil {
		return peHeaders{}, err
	}
	sizeOfOptionalHeader, err := readU16(data, coffOffset+16)
	if err != nil {
		return peHeaders{}, err
	}
	characteristics, err := readU16(data, coffOffset+18)
	if err != nil {
		return peHeaders{}, err
	}
	optionalOffset := coffOffset + 20
	optionalEnd, err := addUsize(optionalOffset, int(sizeOfOptionalHeader), "optional header")
	if err != nil {
		return peHeaders{}, err
	}
	if optionalEnd > len(data) {
		return peHeaders{}, invalidPE("optional header extends past end of file")
	}
	optMagic, err := readU16(data, optionalOffset)
	if err != nil {
		return peHeaders{}, err
	}
	if optMagic == optionalMagicPE32 {
		return peHeaders{}, unsupported("32-bit PE images are not supported")
	}
	if optMagic != optionalMagicPE32P {
		return peHeaders{}, invalidPE(fmt.Sprintf("unknown optional header magic 0x%04x", optMagic))
	}
	entryPoint, err := readU32(data, optionalOffset+16)
	if err != nil {
		return peHeaders{}, err
	}
	imageBase, err := readU64(data, optionalOffset+24)
	if err != nil {
		return peHeaders{}, err
	}
	sectionAlignment, err := readU32(data, optionalOffset+32)
	if err != nil {
		return peHeaders{}, err
	}
	fileAlignment, err := readU32(data, optionalOffset+36)
	if err != nil {
		return peHeaders{}, err
	}
	sizeOfImage, err := readU32(data, optionalOffset+56)
	if err != nil {
		return peHeaders{}, err
	}
	sizeOfHeaders, err := readU32(data, optionalOffset+60)
	if err != nil {
		return peHeaders{}, err
	}
	checksumFileOffset := optionalOffset + 64
	checksum, err := readU32(data, checksumFileOffset)
	if err != nil {
		return peHeaders{}, err
	}
	subsystem, err := readU16(data, optionalOffset+68)
	if err != nil {
		return peHeaders{}, err
	}
	dllCharacteristics, err := readU16(data, optionalOffset+70)
	if err != nil {
		return peHeaders{}, err
	}
	numberOfRvaAndSizes, err := readU32(data, optionalOffset+108)
	if err != nil {
		return peHeaders{}, err
	}
	directoriesOffset := optionalOffset + 112
	available := min((optionalEnd-directoriesOffset)/8, 16)
	declared := min(int(numberOfRvaAndSizes), 16)
	directoryCount := min(available, declared)
	dataDirectories := make([]dataDirectory, 16)
	for index := range directoryCount {
		offset := directoriesOffset + index*8
		va, err := readU32(data, offset)
		if err != nil {
			return peHeaders{}, err
		}
		size, err := readU32(data, offset+4)
		if err != nil {
			return peHeaders{}, err
		}
		dataDirectories[index] = dataDirectory{VirtualAddress: va, Size: size}
	}
	return peHeaders{
		NTOffset: ntOffset, COFFOffset: coffOffset, OptionalOffset: optionalOffset,
		SectionTableOffset: optionalEnd, Machine: machine, NumberOfSections: numberOfSections,
		SizeOfOptionalHeader: sizeOfOptionalHeader, Characteristics: characteristics,
		EntryPoint: entryPoint, ImageBase: imageBase, SectionAlignment: sectionAlignment,
		FileAlignment: fileAlignment, SizeOfImage: sizeOfImage, SizeOfHeaders: sizeOfHeaders,
		Checksum: checksum, ChecksumFileOffset: checksumFileOffset, Subsystem: subsystem,
		DLLCharacteristics: dllCharacteristics, NumberOfRvaAndSizes: numberOfRvaAndSizes,
		DataDirectories: dataDirectories, IsDLL: characteristics&imageFileDLL != 0,
	}, nil
}

func parseSections(data []byte, headers peHeaders) ([]sectionHeader, error) {
	sections := make([]sectionHeader, 0, headers.NumberOfSections)
	for index := range int(headers.NumberOfSections) {
		offset, err := addUsize(headers.SectionTableOffset, index*40, "section table")
		if err != nil {
			return nil, err
		}
		if offset+40 > len(data) {
			return nil, invalidPE("section table extends past end of file")
		}
		virtualSize, err := readU32(data, offset+8)
		if err != nil {
			return nil, err
		}
		virtualAddress, err := readU32(data, offset+12)
		if err != nil {
			return nil, err
		}
		sizeOfRawData, err := readU32(data, offset+16)
		if err != nil {
			return nil, err
		}
		pointerToRawData, err := readU32(data, offset+20)
		if err != nil {
			return nil, err
		}
		characteristics, err := readU32(data, offset+36)
		if err != nil {
			return nil, err
		}
		sections = append(sections, sectionHeader{
			Name: sectionName(data[offset : offset+8]), VirtualSize: virtualSize,
			VirtualAddress: virtualAddress, SizeOfRawData: sizeOfRawData,
			PointerToRawData: pointerToRawData, Characteristics: characteristics,
		})
	}
	return sections, nil
}

func validateSections(data []byte, sections []sectionHeader) error {
	type rawRange struct {
		start, end int
		name       string
	}
	seenRVAs := map[uint32]struct{}{}
	var rawRanges []rawRange
	for _, section := range sections {
		if _, exists := seenRVAs[section.VirtualAddress]; exists {
			return invalidPE(fmt.Sprintf("duplicate section RVA 0x%x", section.VirtualAddress))
		}
		seenRVAs[section.VirtualAddress] = struct{}{}
		if section.SizeOfRawData == 0 {
			continue
		}
		start := int(section.PointerToRawData)
		end, err := addUsize(start, int(section.SizeOfRawData), "section raw end")
		if err != nil {
			return err
		}
		if end > len(data) {
			return invalidPE(fmt.Sprintf("section %s raw range extends past end of file", section.Name))
		}
		rawRanges = append(rawRanges, rawRange{start: start, end: end, name: section.Name})
	}
	slices.SortFunc(rawRanges, func(a, b rawRange) int {
		switch {
		case a.start < b.start:
			return -1
		case a.start > b.start:
			return 1
		default:
			return 0
		}
	})
	for i := 0; i+1 < len(rawRanges); i++ {
		if rawRanges[i+1].start < rawRanges[i].end {
			return invalidPE(fmt.Sprintf("sections %s and %s have overlapping raw ranges", rawRanges[i].name, rawRanges[i+1].name))
		}
	}
	return nil
}

func sectionName(bytes []byte) string {
	end := 0
	for end < len(bytes) && bytes[end] != 0 {
		end++
	}
	return string(bytes[:end])
}

func readU16(data []byte, offset int) (uint16, error) {
	if offset < 0 || offset+2 > len(data) {
		return 0, invalidPE(fmt.Sprintf("read past end of file at offset 0x%x", offset))
	}
	return binary.LittleEndian.Uint16(data[offset : offset+2]), nil
}

func readI32(data []byte, offset int) (int32, error) {
	value, err := readU32(data, offset)
	return int32(value), err
}

func readU32(data []byte, offset int) (uint32, error) {
	if offset < 0 || offset+4 > len(data) {
		return 0, invalidPE(fmt.Sprintf("read past end of file at offset 0x%x", offset))
	}
	return binary.LittleEndian.Uint32(data[offset : offset+4]), nil
}

func readU64(data []byte, offset int) (uint64, error) {
	if offset < 0 || offset+8 > len(data) {
		return 0, invalidPE(fmt.Sprintf("read past end of file at offset 0x%x", offset))
	}
	return binary.LittleEndian.Uint64(data[offset : offset+8]), nil
}
