package xxmi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var sleepyHeader = []byte{
	0, 1, 0, 0, 0, 255, 255, 255, 255, 1, 0, 0, 0,
	0, 0, 0, 0, 6, 1, 0, 0, 0,
}

var zzmiSleepyMagic = []byte{
	85, 110, 209, 150, 116, 209, 131, 206, 149, 110,
	103, 105, 110, 208, 181, 46, 71, 208, 176, 109,
	101, 206, 159, 98, 106, 101, 209, 129, 116,
}

func decodeSleepy(data, magic []byte) ([]byte, error) {
	if len(magic) == 0 || len(data) < len(sleepyHeader)+2 || !bytes.Equal(data[:len(sleepyHeader)], sleepyHeader) {
		return nil, errors.New("invalid Sleepy header")
	}
	length, consumed := binary.Uvarint(data[len(sleepyHeader):])
	if consumed <= 0 || length > 16<<20 {
		return nil, errors.New("invalid Sleepy payload length")
	}
	start := len(sleepyHeader) + consumed
	if length != uint64(len(data)-start-1) || data[len(data)-1] != 11 {
		return nil, errors.New("invalid Sleepy footer or payload length")
	}
	decoded := make([]byte, 0, length)
	eepy := false
	for index, value := range data[start : len(data)-1] {
		position := index % len(magic)
		character := value ^ magic[position]
		if magic[position]&0xc0 == 0xc0 {
			eepy = character != 0
			continue
		}
		if eepy {
			character += 0x40
			eepy = false
		}
		decoded = append(decoded, character)
	}
	if eepy {
		return nil, errors.New("incomplete Sleepy escape")
	}
	return decoded, nil
}

func encodeSleepy(content, magic []byte) ([]byte, error) {
	if len(magic) == 0 || len(content) > 8<<20 {
		return nil, errors.New("invalid Sleepy input size or magic")
	}
	encoded := make([]byte, 0, len(content)*2)
	for _, character := range content {
		position := len(encoded) % len(magic)
		if magic[position]&0xc0 == 0xc0 {
			marker := byte(0)
			if character > 0x40 {
				character -= 0x40
				marker = 1
			}
			encoded = append(encoded, marker^magic[position])
			position = len(encoded) % len(magic)
		}
		encoded = append(encoded, character^magic[position])
	}
	if len(encoded) > 16<<20 {
		return nil, fmt.Errorf("sleepy encoded payload exceeds size limit")
	}
	result := make([]byte, 0, len(sleepyHeader)+binary.MaxVarintLen64+len(encoded)+1)
	result = append(result, sleepyHeader...)
	result = binary.AppendUvarint(result, uint64(len(encoded)))
	result = append(result, encoded...)
	result = append(result, 11)
	return result, nil
}
