package tools

import (
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strings"
)

var modelViewerFormatChannelPattern = regexp.MustCompile(`[RGBA]\d+`)

func modelViewerFormatComponentCount(format string) int {
	normalized := strings.TrimPrefix(strings.ToUpper(format), "DXGI_FORMAT_")
	count := len(modelViewerFormatChannelPattern.FindAllString(normalized, -1))
	if count == 0 {
		return 1
	}
	return count
}

func modelViewerFormatByteSize(format string) int {
	upper := strings.ToUpper(format)
	if upper == "DXGI_FORMAT_R10G10B10A2_UNORM" {
		return 4
	}
	count := modelViewerFormatComponentCount(upper)
	switch {
	case strings.Contains(upper, "32"):
		return count * 4
	case strings.Contains(upper, "16"):
		return count * 2
	case strings.Contains(upper, "8"):
		return count
	default:
		return 0
	}
}

// modelViewerFormatDecoder is a DXGI format resolved once so hot per-vertex
// loops can decode without re-running ToUpper/regex per value.
type modelViewerFormatDecoder struct {
	byteSize   int
	components int
	kind       int
}

const (
	modelViewerFormatF32 = iota
	modelViewerFormatF16
	modelViewerFormatUnorm16
	modelViewerFormatUnorm8
	modelViewerFormatSnorm16
	modelViewerFormatSnorm8
	modelViewerFormatUint32
	modelViewerFormatUint16
	modelViewerFormatUint8
	modelViewerFormatSint32
	modelViewerFormatSint16
	modelViewerFormatSint8
	modelViewerFormatR10G10B10A2
)

func resolveModelViewerFormatDecoder(format string) (modelViewerFormatDecoder, error) {
	upper := strings.ToUpper(format)
	if upper == "DXGI_FORMAT_R10G10B10A2_UNORM" {
		return modelViewerFormatDecoder{byteSize: 4, components: 4, kind: modelViewerFormatR10G10B10A2}, nil
	}
	components := modelViewerFormatComponentCount(upper)
	byteSize := 0
	switch {
	case strings.Contains(upper, "32"):
		byteSize = components * 4
	case strings.Contains(upper, "16"):
		byteSize = components * 2
	case strings.Contains(upper, "8"):
		byteSize = components
	default:
		return modelViewerFormatDecoder{}, fmt.Errorf("unsupported DXGI format: %s", format)
	}
	var kind int
	switch {
	case strings.Contains(upper, "_FLOAT") && strings.Contains(upper, "32"):
		kind = modelViewerFormatF32
	case strings.Contains(upper, "_FLOAT") && strings.Contains(upper, "16"):
		kind = modelViewerFormatF16
	case strings.Contains(upper, "_UNORM") && strings.Contains(upper, "16"):
		kind = modelViewerFormatUnorm16
	case strings.Contains(upper, "_UNORM") && strings.Contains(upper, "8"):
		kind = modelViewerFormatUnorm8
	case strings.Contains(upper, "_SNORM") && strings.Contains(upper, "16"):
		kind = modelViewerFormatSnorm16
	case strings.Contains(upper, "_SNORM") && strings.Contains(upper, "8"):
		kind = modelViewerFormatSnorm8
	case strings.Contains(upper, "_UINT") && strings.Contains(upper, "32"):
		kind = modelViewerFormatUint32
	case strings.Contains(upper, "_UINT") && strings.Contains(upper, "16"):
		kind = modelViewerFormatUint16
	case strings.Contains(upper, "_UINT") && strings.Contains(upper, "8"):
		kind = modelViewerFormatUint8
	case strings.Contains(upper, "_SINT") && strings.Contains(upper, "32"):
		kind = modelViewerFormatSint32
	case strings.Contains(upper, "_SINT") && strings.Contains(upper, "16"):
		kind = modelViewerFormatSint16
	case strings.Contains(upper, "_SINT") && strings.Contains(upper, "8"):
		kind = modelViewerFormatSint8
	default:
		return modelViewerFormatDecoder{}, fmt.Errorf("unsupported DXGI format: %s", format)
	}
	return modelViewerFormatDecoder{byteSize: byteSize, components: components, kind: kind}, nil
}

func readModelViewerDecoded(data []byte, offset int, decoder modelViewerFormatDecoder, out []float32) error {
	if offset < 0 || decoder.byteSize <= 0 || offset+decoder.byteSize > len(data) {
		return fmt.Errorf("out-of-bounds read at offset %d", offset)
	}
	switch decoder.kind {
	case modelViewerFormatR10G10B10A2:
		value := binary.LittleEndian.Uint32(data[offset:])
		out[0] = float32(value&0x3ff) / 1023
		out[1] = float32((value>>10)&0x3ff) / 1023
		out[2] = float32((value>>20)&0x3ff) / 1023
		out[3] = float32((value>>30)&3) / 3
	case modelViewerFormatF32:
		for i := range decoder.components {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[offset+i*4:]))
		}
	case modelViewerFormatF16:
		for i := range decoder.components {
			out[i] = modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[offset+i*2:]))
		}
	case modelViewerFormatUnorm16:
		for i := range decoder.components {
			out[i] = float32(binary.LittleEndian.Uint16(data[offset+i*2:])) / 65535
		}
	case modelViewerFormatUnorm8:
		for i := range decoder.components {
			out[i] = float32(data[offset+i]) / 255
		}
	case modelViewerFormatSnorm16:
		for i := range decoder.components {
			out[i] = max(-1, float32(int16(binary.LittleEndian.Uint16(data[offset+i*2:])))/32767)
		}
	case modelViewerFormatSnorm8:
		for i := range decoder.components {
			out[i] = max(-1, float32(int8(data[offset+i]))/127)
		}
	case modelViewerFormatUint32:
		for i := range decoder.components {
			out[i] = float32(binary.LittleEndian.Uint32(data[offset+i*4:]))
		}
	case modelViewerFormatUint16:
		for i := range decoder.components {
			out[i] = float32(binary.LittleEndian.Uint16(data[offset+i*2:]))
		}
	case modelViewerFormatUint8:
		for i := range decoder.components {
			out[i] = float32(data[offset+i])
		}
	case modelViewerFormatSint32:
		for i := range decoder.components {
			out[i] = float32(int32(binary.LittleEndian.Uint32(data[offset+i*4:])))
		}
	case modelViewerFormatSint16:
		for i := range decoder.components {
			out[i] = float32(int16(binary.LittleEndian.Uint16(data[offset+i*2:])))
		}
	case modelViewerFormatSint8:
		for i := range decoder.components {
			out[i] = float32(int8(data[offset+i]))
		}
	default:
		return fmt.Errorf("unsupported DXGI format")
	}
	return nil
}

func modelViewerReadDXGI(bytes []byte, offset int, format string) ([]float32, error) {
	decoder, err := resolveModelViewerFormatDecoder(format)
	if err != nil {
		return nil, err
	}
	out := make([]float32, decoder.components)
	if readErr := readModelViewerDecoded(bytes, offset, decoder, out); readErr != nil {
		return nil, readErr
	}
	return out, nil
}

func modelViewerHalfToFloat(value uint16) float32 {
	sign := float32(1)
	if value&0x8000 != 0 {
		sign = -1
	}
	exponent := int((value >> 10) & 0x1f)
	fraction := float32(value & 0x03ff)
	if exponent == 0 {
		return sign * float32(math.Ldexp(1, -14)) * fraction / 1024
	}
	if exponent == 31 {
		if fraction != 0 {
			return float32(math.NaN())
		}
		return sign * float32(math.Inf(1))
	}
	return sign * float32(math.Ldexp(1, exponent-15)) * (1 + fraction/1024)
}

func modelViewerDecodeIndices(bytes []byte, format string) ([]uint32, error) {
	upper := strings.ToUpper(format)
	switch {
	case strings.Contains(upper, "R16_UINT"):
		if len(bytes)%2 != 0 {
			return nil, fmt.Errorf("R16_UINT index buffer has an odd byte length")
		}
		out := make([]uint32, len(bytes)/2)
		for i := range out {
			out[i] = uint32(binary.LittleEndian.Uint16(bytes[i*2:]))
		}
		return out, nil
	case strings.Contains(upper, "R32_UINT"), strings.Contains(upper, "UNKNOWN"):
		if len(bytes)%4 != 0 {
			return nil, fmt.Errorf("R32_UINT index buffer byte length is not divisible by 4")
		}
		out := make([]uint32, len(bytes)/4)
		for i := range out {
			out[i] = binary.LittleEndian.Uint32(bytes[i*4:])
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported IB format: %s", format)
	}
}
