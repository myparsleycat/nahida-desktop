package texture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/myparsleycat/ddsutil"

	"nahida.live/desktop/internal/platform"
)

const diagnosticFileLimit = 128 << 20

var ddsRepairMu sync.Mutex

// DDSChannelStats describes stored bytes, before GPU sRGB conversion.
type DDSChannelStats struct {
	Min       byte        `json:"min"`
	Max       byte        `json:"max"`
	Histogram [256]uint32 `json:"histogram"`
}

// DDSInspection separates texture facts from a conditional render-target model.
type DDSInspection struct {
	SHA256              string             `json:"sha256"`
	Format              string             `json:"format"`
	DXGIFormat          uint32             `json:"dxgiFormat"`
	Width               uint32             `json:"width"`
	Height              uint32             `json:"height"`
	Mipmaps             uint32             `json:"mipmaps"`
	SRGB                bool               `json:"srgb"`
	Channels            [4]DDSChannelStats `json:"channelsRGBA"`
	RedQuantizationRisk uint32             `json:"redQuantizationRisk"`
	RiskModel           string             `json:"riskModel"`
}

type diagnosticDDS struct {
	raw     []byte
	surface *ddsutil.Surface
	pixels  []byte
	format  uint32
}

// InspectDDS reports base-mip stored channel values; it does not infer live GPU state.
func InspectDDS(ctx context.Context, path string) (DDSInspection, error) {
	dds, err := loadDiagnosticDDS(ctx, path)
	if err != nil {
		return DDSInspection{}, err
	}
	result := DDSInspection{
		SHA256:     digestDDS(dds.raw),
		Format:     textureImageFormatName(dds.surface.ImageFormat),
		DXGIFormat: dds.format,
		Width:      dds.surface.Width,
		Height:     dds.surface.Height,
		Mipmaps:    dds.surface.Mipmaps,
		SRGB:       linearDDSFormat(dds.format) != dds.format,
		RiskModel:  "Base mip, point samples: nonzero stored red becomes zero after sRGB decoding and 8-bit UNORM rounding. Conditional model, not proof of the live render-target format; filtering may change the boundary.",
	}
	for channel := range result.Channels {
		result.Channels[channel].Min = 255
	}
	for index, value := range dds.pixels {
		stats := &result.Channels[index%4]
		stats.Min, stats.Max = min(stats.Min, value), max(stats.Max, value)
		stats.Histogram[value]++
		if index%4 == 0 && result.SRGB && value > 0 && linearMaskByte(value) == 0 {
			result.RedQuantizationRisk++
		}
	}
	return result, ctx.Err()
}

// PreviewDDS returns a bounded raw-channel PNG without creating a file.
func PreviewDDS(ctx context.Context, path, channel string) ([]byte, error) {
	dds, err := loadDiagnosticDDS(ctx, path)
	if err != nil {
		return nil, err
	}
	channelIndex := strings.Index("rgba", channel)
	if channel != "rgb" && (len(channel) != 1 || channelIndex < 0) {
		return nil, errors.New("preview channel must be rgb, r, g, b, or a")
	}
	width, height := int(dds.surface.Width), int(dds.surface.Height)
	scale := max(1, (max(width, height)+511)/512)
	out := image.NewNRGBA(image.Rect(0, 0, max(1, width/scale), max(1, height/scale)))
	for y := range out.Bounds().Dy() {
		for x := range out.Bounds().Dx() {
			source := (y*scale*width + x*scale) * 4
			target := y*out.Stride + x*4
			copy(out.Pix[target:target+3], dds.pixels[source:source+3])
			if channel != "rgb" {
				value := dds.pixels[source+channelIndex]
				out.Pix[target], out.Pix[target+1], out.Pix[target+2] = value, value, value
			}
			out.Pix[target+3] = 255
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, out); err != nil {
		return nil, err
	}
	return buffer.Bytes(), ctx.Err()
}

// DDSRepairRequest contains only bounded texture operations, never arbitrary binary patches.
type DDSRepairRequest struct {
	Operation        string
	ExpectedSHA256   string
	SourceColorSpace string
	MinimumRed       int
	SelectionPath    string
	SelectionSHA256  string
	WholeTexture     bool
	Apply            bool
}

// DDSRepairResult records the exact before/after identity and retained recovery file.
type DDSRepairResult struct {
	Operation     string `json:"operation"`
	Applied       bool   `json:"applied"`
	BeforeSHA256  string `json:"beforeSHA256"`
	AfterSHA256   string `json:"afterSHA256"`
	BackupPath    string `json:"backupPath,omitempty"`
	ChangedBytes  int    `json:"changedBytes"`
	ChangedPixels int    `json:"changedPixels"`
	OutputBytes   int    `json:"outputBytes"`
}

// RepairDDS prepares a trial or atomically applies it with a unique verified backup.
func RepairDDS(ctx context.Context, path string, request DDSRepairRequest) (DDSRepairResult, error) {
	dds, err := loadDiagnosticDDS(ctx, path)
	if err != nil {
		return DDSRepairResult{}, err
	}
	if request.ExpectedSHA256 == "" || !strings.EqualFold(request.ExpectedSHA256, digestDDS(dds.raw)) {
		return DDSRepairResult{}, errors.New("DDS changed since inspection; inspect again before repairing")
	}
	updated := bytes.Clone(dds.raw)
	changedPixels := 0
	switch request.Operation {
	case "reinterpret_linear":
		if linearDDSFormat(dds.format) == dds.format {
			return DDSRepairResult{}, errors.New("reinterpret_linear requires an sRGB DDS")
		}
		binary.LittleEndian.PutUint32(updated[128:132], linearDDSFormat(dds.format))
	case "linearize_mask", "minimum_red":
		if dds.surface.Mipmaps != 1 {
			return DDSRepairResult{}, errors.New(
				"pixel repair supports one mip only; do not silently discard or regenerate mips",
			)
		}
		pixels := bytes.Clone(dds.pixels)
		if request.Operation == "linearize_mask" {
			if request.SourceColorSpace != "srgb" {
				return DDSRepairResult{}, errors.New(
					"linearize_mask requires explicit sourceColorSpace srgb, including for a previously retagged mask",
				)
			}
			for index := 0; index < len(pixels); index += 4 {
				for channel := range 3 {
					pixels[index+channel] = linearMaskByte(pixels[index+channel])
				}
			}
		} else {
			if dds.format != 28 || request.MinimumRed < 1 || request.MinimumRed > 254 {
				return DDSRepairResult{}, errors.New(
					"minimum_red requires RGBA8_UNORM and minimumRed between 1 and 254",
				)
			}
			if request.WholeTexture == (request.SelectionPath != "") {
				return DDSRepairResult{}, errors.New(
					"choose exactly one of wholeTexture or a binary PNG selection mask",
				)
			}
			selection, err := loadDDSSelection(
				request.SelectionPath,
				request.SelectionSHA256,
				int(dds.surface.Width),
				int(dds.surface.Height),
			)
			if err != nil {
				return DDSRepairResult{}, err
			}
			for index := 0; index < len(pixels); index += 4 {
				if selection == nil || selection[index/4] {
					pixels[index] = max(pixels[index], byte(request.MinimumRed))
				}
			}
		}
		for index := 0; index < len(pixels); index += 4 {
			if !bytes.Equal(pixels[index:index+4], dds.pixels[index:index+4]) {
				changedPixels++
			}
		}
		updated = rgbaDDSBytes(dds.raw, pixels)
	default:
		return DDSRepairResult{}, fmt.Errorf("unsupported DDS repair operation %q", request.Operation)
	}
	result, err := commitDDS(ctx, path, dds.raw, updated, request.Operation, request.Apply)
	result.ChangedPixels = changedPixels
	return result, err
}

// RestoreDDS verifies both files and retains a backup of the state being replaced.
func RestoreDDS(
	ctx context.Context,
	path, backup, expectedSHA256, backupSHA256 string,
	apply bool,
) (DDSRepairResult, error) {
	current, err := readDiagnosticFile(path)
	if err != nil {
		return DDSRepairResult{}, err
	}
	original, err := loadDiagnosticDDS(ctx, backup)
	if err != nil {
		return DDSRepairResult{}, err
	}
	if !strings.EqualFold(expectedSHA256, digestDDS(current)) ||
		!strings.EqualFold(backupSHA256, digestDDS(original.raw)) {
		return DDSRepairResult{}, errors.New("current or backup DDS hash mismatch; no restore performed")
	}
	return commitDDS(ctx, path, current, original.raw, "restore", apply)
}

func loadDiagnosticDDS(ctx context.Context, path string) (diagnosticDDS, error) {
	if err := ctx.Err(); err != nil {
		return diagnosticDDS{}, err
	}
	raw, err := readDiagnosticFile(path)
	if err != nil {
		return diagnosticDDS{}, err
	}
	if len(raw) < 148 || string(raw[:4]) != "DDS " || string(raw[84:88]) != "DX10" ||
		binary.LittleEndian.Uint32(raw[4:8]) != 124 || binary.LittleEndian.Uint32(raw[76:80]) != 32 {
		return diagnosticDDS{}, errors.New("diagnostics require a valid DX10 DDS header")
	}
	u32 := func(offset int) uint32 { return binary.LittleEndian.Uint32(raw[offset : offset+4]) }
	format, width, height := u32(128), u32(16), u32(12)
	if u32(132) != 3 || u32(136)&4 != 0 || u32(140) != 1 || u32(24) > 1 || u32(112) != 0 {
		return diagnosticDDS{}, errors.New("diagnostics support a single 2D texture, not arrays, cubes, or volumes")
	}
	if width == 0 || height == 0 || uint64(width)*uint64(height) > 16<<20 || u32(28) > 15 {
		return diagnosticDDS{}, errors.New("DDS dimensions or mip count exceed diagnostic limits")
	}
	switch linearDDSFormat(format) {
	case 28, 71, 74, 77, 98:
	default:
		return diagnosticDDS{}, fmt.Errorf(
			"unsupported diagnostic DXGI format %d; supported: RGBA8 and BC1/2/3/7 UNORM or sRGB",
			format,
		)
	}
	dds, err := ddsutil.Read(bytes.NewReader(raw))
	if err != nil {
		return diagnosticDDS{}, fmt.Errorf("read DDS: %w", err)
	}
	surface, err := ddsutil.SurfaceFromDds(dds)
	if err != nil {
		return diagnosticDDS{}, fmt.Errorf("read DDS surface: %w", err)
	}
	decoded, err := surface.DecodeLayersMipmapsRgba8(0, 1, 0, 1)
	if err != nil {
		return diagnosticDDS{}, fmt.Errorf("decode DDS base mip: %w", err)
	}
	return diagnosticDDS{raw: raw, surface: surface, pixels: decoded.Data, format: format}, ctx.Err()
}

func readDiagnosticFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > diagnosticFileLimit {
		return nil, errors.New("diagnostic input must be a regular file no larger than 128 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, diagnosticFileLimit+1))
	if len(data) > diagnosticFileLimit {
		return nil, errors.New("diagnostic input exceeds 128 MiB")
	}
	return data, err
}

func linearDDSFormat(format uint32) uint32 {
	switch format {
	case 29, 72, 75, 78, 99:
		return format - 1
	default:
		return format
	}
}

func linearMaskByte(value byte) byte {
	x := float64(value) / 255
	if x <= 0.04045 {
		x /= 12.92
	} else {
		x = math.Pow((x+0.055)/1.055, 2.4)
	}
	return byte(math.Round(x * 255))
}

func rgbaDDSBytes(source, pixels []byte) []byte {
	result := make([]byte, 148+len(pixels))
	copy(result, source[:148])
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(result[offset:offset+4], value) }
	put(8, 0x100f) // CAPS | HEIGHT | WIDTH | PITCH | PIXELFORMAT
	put(20, binary.LittleEndian.Uint32(result[16:20])*4)
	put(24, 0)
	put(28, 1)
	put(108, 0x1000) // DDSCAPS_TEXTURE
	put(128, 28)     // RGBA8_UNORM: no compression or implicit sRGB conversion.
	copy(result[148:], pixels)
	return result
}

func loadDDSSelection(path, expectedSHA256 string, width, height int) ([]bool, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readDiagnosticFile(path)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(expectedSHA256, digestDDS(data)) {
		return nil, errors.New("selection PNG hash mismatch; inspect or regenerate the selection")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != width || config.Height != height {
		return nil, errors.New("selection must be a PNG with exactly the DDS dimensions")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	selection := make([]bool, width*height)
	count := 0
	for y := range height {
		for x := range width {
			r, g, b, a := img.At(x, y).RGBA()
			if a != 65535 || r != g || r != b || (r != 0 && r != 65535) {
				return nil, errors.New("selection must contain only opaque black and white pixels")
			}
			selection[y*width+x] = r != 0
			if r != 0 {
				count++
			}
		}
	}
	if count == 0 {
		return nil, errors.New("selection mask is empty")
	}
	return selection, nil
}

func digestDDS(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func commitDDS(
	ctx context.Context,
	path string,
	before, after []byte,
	operation string,
	apply bool,
) (result DDSRepairResult, err error) {
	result = DDSRepairResult{
		Operation:    operation,
		BeforeSHA256: digestDDS(before),
		AfterSHA256:  digestDDS(after),
		OutputBytes:  len(after),
	}
	for index := range max(len(before), len(after)) {
		if index >= len(before) || index >= len(after) || before[index] != after[index] {
			result.ChangedBytes++
		}
	}
	if !apply || result.ChangedBytes == 0 {
		return result, ctx.Err()
	}
	// Serialize our commits, then recheck the observed bytes before making a backup.
	ddsRepairMu.Lock()
	defer ddsRepairMu.Unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	current, err := readDiagnosticFile(path)
	if err != nil || !bytes.Equal(current, before) {
		return result, errors.Join(err, errors.New("DDS changed during repair; not replaced"))
	}
	backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".nahida-backup-*")
	if err != nil {
		return result, fmt.Errorf("create DDS backup: %w", err)
	}
	result.BackupPath = backup.Name()
	defer func() {
		if err != nil {
			err = fmt.Errorf(
				"DDS %s failed for %s (backup retained at %s): %w",
				operation,
				path,
				result.BackupPath,
				err,
			)
		}
	}()
	_, writeErr := backup.Write(before)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		return result, err
	}
	verified, err := readDiagnosticFile(result.BackupPath)
	if err != nil || !bytes.Equal(verified, before) {
		return result, errors.Join(err, errors.New("DDS backup verification failed"))
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".nahida-dds-*")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	_, writeErr = temp.Write(after)
	err = errors.Join(writeErr, temp.Sync(), temp.Close())
	if err != nil {
		return result, err
	}
	verified, err = readDiagnosticFile(temp.Name())
	if err != nil || !bytes.Equal(verified, after) {
		return result, errors.Join(err, errors.New("DDS staged output verification failed"))
	}
	current, err = readDiagnosticFile(path)
	if err != nil || !bytes.Equal(current, before) {
		return result, errors.Join(err, errors.New("DDS changed during repair; not replaced"))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := platform.ReplaceAtomic(temp.Name(), path); err != nil {
		return result, err
	}
	result.Applied = true
	verified, err = readDiagnosticFile(path)
	if err != nil || !bytes.Equal(verified, after) {
		return result, errors.Join(
			err,
			errors.New("DDS post-write verification failed; restore from the retained backup"),
		)
	}
	return result, nil
}
