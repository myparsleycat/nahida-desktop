package pepad

const (
	textRVA  uint32 = 0x1000
	textRaw         = 0x200
	rdataRVA uint32 = 0x2000
	rdataRaw        = 0x400
	relocRVA uint32 = 0x3000
	relocRaw        = 0x600
)

const (
	sectionAlignment uint32 = 0x1000
	fileAlignment    uint32 = 0x200
)

type sectionSpec struct {
	name            string
	rva             uint32
	raw             int
	virtualSize     uint32
	rawSize         uint32
	characteristics uint32
	data            []byte
}

func MinimalDLL(text []byte) []byte {
	return buildPE([]sectionSpec{textSection(text)}, [16]*[2]uint32{}, true, 0x8664, 0x20b)
}

func RetThenInt3Padding(length int) []byte {
	text := []byte{0xc3}
	for range length {
		text = append(text, 0xcc)
	}
	for len(text) < 0x200 {
		text = append(text, 0xcc)
	}
	return text
}

func textSection(data []byte) sectionSpec {
	out := append([]byte(nil), data...)
	for len(out) < 0x200 {
		out = append(out, 0xcc)
	}
	return sectionSpec{
		name: ".text", rva: textRVA, raw: textRaw, virtualSize: 0x200, rawSize: 0x200,
		characteristics: 0x60000020, data: out,
	}
}

func rdataSection(data []byte) sectionSpec {
	out := append([]byte(nil), data...)
	for len(out) < 0x200 {
		out = append(out, 0)
	}
	return sectionSpec{
		name: ".rdata", rva: rdataRVA, raw: rdataRaw, virtualSize: 0x200, rawSize: 0x200,
		characteristics: 0x40000040, data: out,
	}
}

func relocSection(data []byte) sectionSpec {
	out := append([]byte(nil), data...)
	for len(out) < 0x200 {
		out = append(out, 0)
	}
	return sectionSpec{
		name: ".reloc", rva: relocRVA, raw: relocRaw, virtualSize: 0x200, rawSize: 0x200,
		characteristics: 0x42000040, data: out,
	}
}

func buildPE(sections []sectionSpec, directories [16]*[2]uint32, dll bool, machine, optionalMagic uint16) []byte {
	optionalSize := 0xf0
	nt := 0x80
	sectionTable := nt + 4 + 20 + optionalSize
	headersSize := 0x200
	fileLen := headersSize
	for _, section := range sections {
		if end := section.raw + int(section.rawSize); end > fileLen {
			fileLen = end
		}
	}
	bytes := make([]byte, fileLen)
	putU16(bytes, 0, 0x5a4d)
	putU32(bytes, 0x3c, uint32(nt))
	putU32(bytes, nt, 0x00004550)
	coff := nt + 4
	putU16(bytes, coff, machine)
	putU16(bytes, coff+2, uint16(len(sections)))
	putU16(bytes, coff+16, uint16(optionalSize))
	characteristics := uint16(0x0002 | 0x0020)
	if dll {
		characteristics |= 0x2000
	}
	putU16(bytes, coff+18, characteristics)
	opt := coff + 20
	putU16(bytes, opt, optionalMagic)
	bytes[opt+2] = 14
	putU32(bytes, opt+4, 0x200)
	putU32(bytes, opt+8, 0x200)
	putU32(bytes, opt+16, textRVA)
	putU32(bytes, opt+20, textRVA)
	putU64(bytes, opt+24, 0x0000000180000000)
	putU32(bytes, opt+32, sectionAlignment)
	putU32(bytes, opt+36, fileAlignment)
	putU16(bytes, opt+40, 6)
	putU16(bytes, opt+48, 6)
	imageEnd := textRVA + sectionAlignment
	for _, section := range sections {
		if end := alignU32(section.rva+section.virtualSize, sectionAlignment); end > imageEnd {
			imageEnd = end
		}
	}
	putU32(bytes, opt+56, imageEnd)
	putU32(bytes, opt+60, uint32(headersSize))
	putU16(bytes, opt+68, 3)
	putU16(bytes, opt+70, 0x8160)
	putU64(bytes, opt+72, 0x100000)
	putU64(bytes, opt+80, 0x1000)
	putU64(bytes, opt+88, 0x100000)
	putU64(bytes, opt+96, 0x1000)
	putU32(bytes, opt+108, 16)
	for index, directory := range directories {
		if directory == nil {
			continue
		}
		putU32(bytes, opt+112+index*8, directory[0])
		putU32(bytes, opt+116+index*8, directory[1])
	}
	for index, section := range sections {
		offset := sectionTable + index*40
		var name [8]byte
		copy(name[:], section.name)
		copy(bytes[offset:offset+8], name[:])
		putU32(bytes, offset+8, section.virtualSize)
		putU32(bytes, offset+12, section.rva)
		putU32(bytes, offset+16, section.rawSize)
		putU32(bytes, offset+20, uint32(section.raw))
		putU32(bytes, offset+36, section.characteristics)
		copy(bytes[section.raw:section.raw+int(section.rawSize)], section.data[:section.rawSize])
	}
	return bytes
}

func alignU32(value, alignment uint32) uint32 {
	return (value + alignment - 1) / alignment * alignment
}

func putU16(bytes []byte, offset int, value uint16) {
	bytes[offset] = byte(value)
	bytes[offset+1] = byte(value >> 8)
}

func putU32(bytes []byte, offset int, value uint32) {
	bytes[offset] = byte(value)
	bytes[offset+1] = byte(value >> 8)
	bytes[offset+2] = byte(value >> 16)
	bytes[offset+3] = byte(value >> 24)
}

func putU64(bytes []byte, offset int, value uint64) {
	putU32(bytes, offset, uint32(value))
	putU32(bytes, offset+4, uint32(value>>32))
}
