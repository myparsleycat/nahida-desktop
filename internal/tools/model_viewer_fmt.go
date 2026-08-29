package tools

import (
	"fmt"
	"strconv"
	"strings"
)

type modelViewerFmtElement struct {
	SemanticName         string
	SemanticIndex        int
	Format               string
	InputSlot            int
	AlignedByteOffset    int
	InputSlotClass       string
	InstanceDataStepRate int
}

type modelViewerFmtLayout struct {
	Stride      int
	Topology    string
	IndexFormat string
	Elements    []modelViewerFmtElement
}

// modelViewerLayoutKey is a stable identity for a resolved layout, used to key
// cached geometry extractions.
func modelViewerLayoutKey(layout modelViewerFmtLayout) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%d|%s|%s", layout.Stride, layout.Topology, layout.IndexFormat)
	for _, element := range layout.Elements {
		fmt.Fprintf(&builder, "|%s,%d,%s,%d,%d,%s,%d", element.SemanticName, element.SemanticIndex, element.Format, element.InputSlot, element.AlignedByteOffset, element.InputSlotClass, element.InstanceDataStepRate)
	}
	return builder.String()
}

func parseModelViewerFmt(text string, fallbackStride int, fallbackIndexFormat string) (modelViewerFmtLayout, error) {
	layout := modelViewerFmtLayout{Stride: fallbackStride, Topology: "trianglelist", IndexFormat: fallbackIndexFormat}
	if layout.IndexFormat == "" {
		layout.IndexFormat = "DXGI_FORMAT_R32_UINT"
	}
	type pendingElement struct {
		value  modelViewerFmtElement
		active bool
	}
	current := pendingElement{}
	appendOffset := 0
	finish := func() {
		if !current.active {
			return
		}
		if current.value.Format == "" {
			current.value.Format = "DXGI_FORMAT_UNKNOWN"
		}
		if current.value.InputSlotClass == "" {
			current.value.InputSlotClass = "per-vertex"
		}
		if !strings.EqualFold(current.value.InputSlotClass, "per-instance") {
			layout.Elements = append(layout.Elements, current.value)
		}
		appendOffset = current.value.AlignedByteOffset + modelViewerFormatByteSize(current.value.Format)
		current = pendingElement{}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "stride:"):
			value, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "stride:")))
			if err != nil {
				return layout, fmt.Errorf("parse FMT stride: %w", err)
			}
			layout.Stride = value
		case strings.HasPrefix(line, "topology:"):
			layout.Topology = strings.TrimSpace(strings.TrimPrefix(line, "topology:"))
		case strings.HasPrefix(line, "format:"):
			layout.IndexFormat = strings.TrimSpace(strings.TrimPrefix(line, "format:"))
		case strings.HasPrefix(line, "element["):
			finish()
			current.active = true
		default:
			if !current.active {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			switch key {
			case "SemanticName":
				current.value.SemanticName = value
			case "SemanticIndex":
				current.value.SemanticIndex, _ = strconv.Atoi(value)
			case "Format":
				current.value.Format = value
			case "InputSlot":
				current.value.InputSlot, _ = strconv.Atoi(value)
			case "AlignedByteOffset":
				if value == "append" {
					current.value.AlignedByteOffset = appendOffset
				} else {
					current.value.AlignedByteOffset, _ = strconv.Atoi(value)
				}
			case "InputSlotClass":
				current.value.InputSlotClass = value
			case "InstanceDataStepRate":
				current.value.InstanceDataStepRate, _ = strconv.Atoi(value)
			}
		}
	}
	finish()
	if layout.Stride <= 0 {
		return layout, fmt.Errorf("invalid FMT stride %d", layout.Stride)
	}
	return layout, nil
}

func extractModelViewerFmtFromVB0(text string, stride int, indexFormat string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := []string{fmt.Sprintf("stride: %d", stride), "topology: trianglelist", "format: " + indexFormat}
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "element[") {
			continue
		}
		out = append(out, line)
		for j := 1; j <= 7 && i+j < len(lines); j++ {
			out = append(out, "  "+strings.TrimSpace(lines[i+j]))
		}
	}
	return strings.Join(out, "\n")
}
