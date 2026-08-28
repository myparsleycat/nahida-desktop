package pepad

import (
	"crypto/sha256"
	"encoding/hex"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func computePEChecksum(data []byte, checksumOffset int) uint32 {
	var sum uint64
	for index := 0; index < len(data); index += 2 {
		skip := index == checksumOffset || index == checksumOffset+2
		var word uint64
		if index+1 < len(data) {
			word = uint64(data[index]) | uint64(data[index+1])<<8
		} else {
			word = uint64(data[index])
		}
		if !skip {
			sum += word
			sum = (sum & 0xffff) + (sum >> 16)
		}
	}
	sum = (sum & 0xffff) + (sum >> 16)
	sum += uint64(len(data))
	return uint32(sum)
}
