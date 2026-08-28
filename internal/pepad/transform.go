package pepad

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type patch struct {
	CandidateID int
	RVA         uint32
	FileOffset  int
	Replacement []byte
	Template    string
}

func planPatches(candidates []approvedCandidate, input []byte, opts Options) ([]patch, error) {
	limit := len(candidates)
	if opts.MaximumMutations != nil && *opts.MaximumMutations < limit {
		limit = *opts.MaximumMutations
	}
	inputHash := sha256.Sum256(input)
	var patches []patch
	for _, candidate := range candidates[:limit] {
		rng := newChaCha20(candidateSeed(opts.Seed, inputHash[:], candidate))
		replacement, template, ok := renderTemplate(candidate.Length, rng)
		if !ok {
			continue
		}
		if len(replacement) != candidate.Length {
			return nil, validationErr("template length did not match candidate length")
		}
		patches = append(patches, patch{
			CandidateID: candidate.ID, RVA: candidate.RVA, FileOffset: candidate.FileOffset,
			Replacement: replacement, Template: template,
		})
	}
	return patches, nil
}

func candidateSeed(seed uint64, inputHash []byte, candidate approvedCandidate) [32]byte {
	h := sha256.New()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], seed)
	h.Write(buf[:])
	h.Write(inputHash)
	binary.LittleEndian.PutUint64(buf[:], uint64(candidate.ID))
	h.Write(buf[:])
	binary.LittleEndian.PutUint32(buf[:4], candidate.RVA)
	h.Write(buf[:4])
	binary.LittleEndian.PutUint64(buf[:], uint64(candidate.FileOffset))
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(candidate.Length))
	h.Write(buf[:])
	h.Write([]byte(programVersion))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func applyPatches(input []byte, patches []patch) ([]byte, error) {
	output := append([]byte(nil), input...)
	for _, item := range patches {
		end, err := addUsize(item.FileOffset, len(item.Replacement), "patch file range")
		if err != nil {
			return nil, err
		}
		if end > len(output) {
			return nil, validationErr(fmt.Sprintf("patch for candidate %d extends past end of file", item.CandidateID))
		}
		copy(output[item.FileOffset:end], item.Replacement)
	}
	return output, nil
}

func renderTemplate(length int, rng *chacha20) ([]byte, string, bool) {
	if length < 2 {
		return nil, "", false
	}
	out := make([]byte, 0, length)
	var name string
	if length <= 129 {
		out = append(out, 0xeb, byte(length-2))
		name = "short_jmp_over_nop_payload"
		out = fillNOPPayload(out, length-2, rng)
	} else {
		out = append(out, 0xe9)
		disp := uint32(length - 5)
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], disp)
		out = append(out, buf[:]...)
		name = "near_jmp_over_nop_payload"
		out = fillNOPPayload(out, length-5, rng)
	}
	return out, name, true
}

func fillNOPPayload(out []byte, remaining int, rng *chacha20) []byte {
	nops := [][]byte{
		{0x90},
		{0x66, 0x90},
		{0x0f, 0x1f, 0x00},
		{0x0f, 0x1f, 0x40, 0x00},
		{0x0f, 0x1f, 0x44, 0x00, 0x00},
		{0x66, 0x0f, 0x1f, 0x44, 0x00, 0x00},
		{0x0f, 0x1f, 0x80, 0x00, 0x00, 0x00, 0x00},
		{0x0f, 0x1f, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0x66, 0x0f, 0x1f, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00},
	}
	for remaining > 0 {
		var fitting [][]byte
		for _, candidate := range nops {
			if len(candidate) <= remaining {
				fitting = append(fitting, candidate)
			}
		}
		selected := fitting[int(rng.nextU32())%len(fitting)]
		out = append(out, selected...)
		remaining -= len(selected)
	}
	return out
}

// chacha20 matches rand_chacha::ChaCha20Rng::from_seed + next_u32:
// 20-round ChaCha, 32-byte key, 64-bit counter/stream initialized to 0,
// little-endian u32 words, 4-block refill.
type chacha20 struct {
	state [16]uint32
	buf   [64]uint32
	index int
}

func newChaCha20(seed [32]byte) *chacha20 {
	var c chacha20
	c.state[0] = 0x61707865
	c.state[1] = 0x3320646e
	c.state[2] = 0x79622d32
	c.state[3] = 0x6b206574
	for i := range 8 {
		c.state[4+i] = binary.LittleEndian.Uint32(seed[i*4 : i*4+4])
	}
	c.refill()
	return &c
}

func (c *chacha20) nextU32() uint32 {
	if c.index >= len(c.buf) {
		c.refill()
	}
	value := c.buf[c.index]
	c.index++
	return value
}

func (c *chacha20) refill() {
	for block := range 4 {
		var working [16]uint32
		copy(working[:], c.state[:])
		for range 10 {
			quarter(&working, 0, 4, 8, 12)
			quarter(&working, 1, 5, 9, 13)
			quarter(&working, 2, 6, 10, 14)
			quarter(&working, 3, 7, 11, 15)
			quarter(&working, 0, 5, 10, 15)
			quarter(&working, 1, 6, 11, 12)
			quarter(&working, 2, 7, 8, 13)
			quarter(&working, 3, 4, 9, 14)
		}
		for i := range 16 {
			c.buf[block*16+i] = working[i] + c.state[i]
		}
		c.state[12]++
		if c.state[12] == 0 {
			c.state[13]++
		}
	}
	c.index = 0
}

func quarter(s *[16]uint32, a, b, c, d int) {
	s[a] += s[b]
	s[d] = rotl32(s[d]^s[a], 16)
	s[c] += s[d]
	s[b] = rotl32(s[b]^s[c], 12)
	s[a] += s[b]
	s[d] = rotl32(s[d]^s[a], 8)
	s[c] += s[d]
	s[b] = rotl32(s[b]^s[c], 7)
}

func rotl32(value uint32, n uint) uint32 {
	return value<<n | value>>(32-n)
}
