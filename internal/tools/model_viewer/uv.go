package modelviewer

import (
	"sort"
)

func detectModelViewerUVBest(data []byte, vertexStride, baseOffset, texcoordStride int) (int, string) {
	total := 0
	if vertexStride > 0 {
		total = len(data) / vertexStride
	}
	if total == 0 {
		return baseOffset + 4, "DXGI_FORMAT_R16G16_FLOAT"
	}
	type score struct {
		live            bool
		inRange, spread float64
		offset          int
		format          string
	}
	var scores []score
	step := max(1, total/4096)
	for _, relative := range []int{0, 4} {
		for _, format := range []string{"DXGI_FORMAT_R16G16_FLOAT", "DXGI_FORMAT_R32G32_FLOAT"} {
			decoder, decoderErr := resolveModelViewerFormatDecoder(format)
			if decoderErr != nil || decoder.byteSize <= 0 {
				continue
			}
			size := decoder.byteSize
			if relative+size > texcoordStride {
				continue
			}
			var us, vs []float64
			values := make([]float32, max(decoder.components, 2))
			sampled := 0
			for vertex := 0; vertex < total; vertex += step {
				if err := readModelViewerDecoded(
					data,
					vertex*vertexStride+baseOffset+relative,
					decoder,
					values,
				); err != nil {
					break
				}
				sampled++
				u, v := float64(values[0]), float64(values[1])
				if u >= -.01 && u <= 2 && v >= -.01 && v <= 2 {
					us = append(us, u)
					vs = append(vs, v)
				}
			}
			if sampled == 0 || len(us) == 0 {
				continue
			}
			inRange := float64(len(us)) / float64(sampled)
			if inRange < .95 {
				continue
			}
			minU, maxU, minV, maxV := us[0], us[0], vs[0], vs[0]
			for i := range us {
				minU = min(minU, us[i])
				maxU = max(maxU, us[i])
				minV = min(minV, vs[i])
				maxV = max(maxV, vs[i])
			}
			du, dv := maxU-minU, maxV-minV
			scores = append(
				scores,
				score{
					live:    du >= 1e-4 && dv >= 1e-4,
					inRange: inRange,
					spread:  du + dv,
					offset:  baseOffset + relative,
					format:  format,
				},
			)
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].live != scores[j].live {
			return scores[i].live
		}
		if scores[i].inRange != scores[j].inRange {
			return scores[i].inRange > scores[j].inRange
		}
		return scores[i].spread > scores[j].spread
	})
	if len(scores) > 0 {
		return scores[0].offset, scores[0].format
	}
	if texcoordStride >= 8 {
		return baseOffset + 4, "DXGI_FORMAT_R16G16_FLOAT"
	}
	return baseOffset, "DXGI_FORMAT_R16G16_FLOAT"
}
