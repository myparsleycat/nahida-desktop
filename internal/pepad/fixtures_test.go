package pepad

func minimalDLL(text []byte) []byte {
	return MinimalDLL(text)
}

func exeFixture(text []byte) []byte {
	return buildPE([]sectionSpec{textSection(text)}, [16]*[2]uint32{}, false, 0x8664, 0x20b)
}

func pe32Fixture(text []byte) []byte {
	return buildPE([]sectionSpec{textSection(text)}, [16]*[2]uint32{}, true, 0x014c, 0x10b)
}

func signedFixture(text []byte) []byte {
	var directories [16]*[2]uint32
	directories[securityDirectory] = &[2]uint32{0x800, 0x80}
	bytes := buildPE([]sectionSpec{textSection(text)}, directories, true, 0x8664, 0x20b)
	out := make([]byte, 0x880)
	copy(out, bytes)
	return out
}

func exceptionFixture(text []byte, begin, end uint32) []byte {
	rdata := make([]byte, 0x200)
	rdata[0] = 1
	pdata := make([]byte, 0x200)
	putU32(pdata, 0, begin)
	putU32(pdata, 4, end)
	putU32(pdata, 8, rdataRVA)
	var directories [16]*[2]uint32
	directories[exceptionDirectory] = &[2]uint32{rdataRVA + 0x200, 12}
	pdataSection := sectionSpec{
		name: ".pdata", rva: rdataRVA + 0x200, raw: rdataRaw + 0x200,
		virtualSize: 0x200, rawSize: 0x200, characteristics: 0x40000040, data: pdata,
	}
	return buildPE([]sectionSpec{textSection(text), rdataSection(rdata), pdataSection}, directories, true, 0x8664, 0x20b)
}

func relocationFixture(text []byte, targetRVA uint32) []byte {
	reloc := make([]byte, 0x200)
	page := targetRVA &^ 0x0fff
	offset := targetRVA & 0x0fff
	putU32(reloc, 0, page)
	putU32(reloc, 4, 12)
	putU16(reloc, 8, 0xa000|uint16(offset))
	var directories [16]*[2]uint32
	directories[baserelocDirectory] = &[2]uint32{relocRVA, 12}
	return buildPE([]sectionSpec{textSection(text), relocSection(reloc)}, directories, true, 0x8664, 0x20b)
}

func overlappingSectionsFixture(text []byte) []byte {
	sections := []sectionSpec{textSection(text), rdataSection(make([]byte, 0x200))}
	sections[1].raw = textRaw + 0x100
	return buildPE(sections, [16]*[2]uint32{}, true, 0x8664, 0x20b)
}

func noPaddingText() []byte {
	text := []byte{0xc3}
	for len(text) < 0x200 {
		text = append(text, byte(0x40+(len(text)%0x20)))
	}
	return text
}

func retThenInt3Padding(length int) []byte {
	return RetThenInt3Padding(length)
}
