package texture

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/myparsleycat/ddsutil"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

const (
	// Smaller textures cost little video memory, and many of them are lookup or UI data that a lossy
	// format would damage.
	uncompressedMinPixels = 1024 * 1024
	ddsDX10HeaderEnd      = 148
	ddsVolumeFlag         = 0x200000
	uncompressedWorkers   = 4
)

// B8G8R8X8 is left out because ddsutil cannot decode it, so offering it would only fail.
var uncompressedTextureFormats = []string{
	"DXGI_FORMAT_R8G8B8A8_UNORM", "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB",
	"DXGI_FORMAT_B8G8R8A8_UNORM", "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB",
}

// UncompressedTexture is a large 32-bit DDS that a block-compressed format would shrink.
type UncompressedTexture struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	Format       string `json:"format"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"fileSize"`

	srgb bool
	// info is the state of the file the header was read from.
	info os.FileInfo
}

// FindUncompressed lists the uncompressed textures of the mods under root that 3DMigoto loads, so
// it leaves out everything below a DISABLED entry. Unreadable entries are skipped.
func FindUncompressed(ctx context.Context, root string) ([]UncompressedTexture, error) {
	release, err := diskio.AcquireDir(ctx, root)
	if err != nil {
		return nil, err
	}
	defer release()

	found := []UncompressedTexture{}
	err = walkLoadedDDS(ctx, root, map[string]struct{}{}, func(path string) {
		texture, ok := readUncompressedTexture(path)
		if !ok {
			return
		}
		if rel, err := filepath.Rel(root, path); err == nil {
			texture.RelativePath = rel
		}
		found = append(found, texture)
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(found, func(i, j int) bool {
		return found[i].Width*found[i].Height > found[j].Width*found[j].Height
	})
	return found, nil
}

// walkLoadedDDS visits the DDS files below dir. It enters junctions and symbolic links, which
// filepath.WalkDir leaves alone although a Mods folder or a mod in it is often one, and visited
// keeps a link that points back at one of its parents from looping.
func walkLoadedDDS(ctx context.Context, dir string, visited map[string]struct{}, visit func(path string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	final, err := platform.FinalPath(dir)
	if err != nil {
		return nil //nolint:nilerr // An unreadable mod must not hide the textures of the others.
	}
	key := strings.ToLower(final)
	if _, seen := visited[key]; seen {
		return nil
	}
	visited[key] = struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil //nolint:nilerr // An unreadable mod must not hide the textures of the others.
	}

	for _, entry := range entries {
		if strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		isDir := entry.IsDir()
		if !isDir && !entry.Type().IsRegular() {
			info, err := os.Stat(path)
			isDir = err == nil && info.IsDir()
		}
		if !isDir {
			if isDDSFilePath(path) {
				visit(path)
			}
			continue
		}
		if err := walkLoadedDDS(ctx, path, visited, visit); err != nil {
			return err
		}
	}
	return nil
}

// readUncompressedTexture reads only the header, because a scan runs before every launch.
func readUncompressedTexture(path string) (UncompressedTexture, bool) {
	file, err := os.Open(path)
	if err != nil {
		return UncompressedTexture{}, false
	}
	defer func() { _ = file.Close() }()
	return inspectUncompressedTexture(file, path)
}

func inspectUncompressedTexture(file *os.File, path string) (UncompressedTexture, bool) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return UncompressedTexture{}, false
	}
	header := make([]byte, ddsDX10HeaderEnd)
	read, err := io.ReadFull(file, header)
	if err != nil && read < 132 {
		return UncompressedTexture{}, false
	}
	header = header[:read]

	metadata, err := parseDDS(header)
	if err != nil || metadata.layers != 1 || !contains(uncompressedTextureFormats, metadata.format) {
		return UncompressedTexture{}, false
	}
	u := func(offset int) uint32 { return binary.LittleEndian.Uint32(header[offset : offset+4]) }
	if u(112)&ddsVolumeFlag != 0 {
		return UncompressedTexture{}, false
	}
	if u(80)&ddpfFourCC != 0 && (len(header) < ddsDX10HeaderEnd || u(132) != 3 || u(140) > 1) {
		return UncompressedTexture{}, false
	}
	// Direct3D 11 refuses a block-compressed texture whose size is not a multiple of the block.
	if metadata.width*metadata.height < uncompressedMinPixels || metadata.width%4 != 0 || metadata.height%4 != 0 {
		return UncompressedTexture{}, false
	}
	return UncompressedTexture{
		Path: path, RelativePath: filepath.Base(path), Format: metadata.format,
		Width: metadata.width, Height: metadata.height, FileSize: info.Size(),
		srgb: metadata.colorSpace == "srgb", info: info,
	}, true
}

// loadUncompressedTexture checks and reads the texture through one handle, so the pixels belong to
// the header that qualified. It returns no DDS for a file that does not qualify.
func loadUncompressedTexture(path string) (UncompressedTexture, *ddsutil.Dds, error) {
	file, err := os.Open(path)
	if err != nil {
		// A texture that is gone is skipped like one that changed.
		return UncompressedTexture{}, nil, nil
	}
	defer func() { _ = file.Close() }()
	texture, ok := inspectUncompressedTexture(file, path)
	if !ok {
		return UncompressedTexture{}, nil, nil
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return UncompressedTexture{}, nil, textureError("Failed to read DDS file '%s': %s", path, err)
	}
	dds, err := ddsutil.Read(file)
	if err != nil {
		return UncompressedTexture{}, nil, textureError("Failed to read DDS file '%s': %s", path, err)
	}
	return texture, dds, nil
}

// textureChanged compares through a handle, because the directory entry of a hard-linked file can
// lag behind its real size and time.
func textureChanged(before os.FileInfo, path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return true
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	return err != nil || !os.SameFile(before, after) || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime())
}

// CompressUncompressed converts each texture to BC7 in place and keeps its color space, so the
// shaders sample the same values. A texture that changed since it was listed is checked again and
// skipped when it no longer qualifies. A failed file does not stop the others, and a cancelled run
// still finishes the files it is already encoding.
func CompressUncompressed(
	ctx context.Context,
	textures []UncompressedTexture,
	backup bool,
	progress func(done, total int, path string),
) (TextureResizeResult, error) {
	files := make([]TextureResizeFileResult, len(textures))
	var done int
	var mu sync.Mutex
	// The encoder is single-threaded and a 5120x5120 texture takes it about half a minute, while
	// each one holds its pixels twice over, so only a few run at once.
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(min(uncompressedWorkers, runtime.NumCPU()))
	for index, listed := range textures {
		group.Go(func() error {
			file, err := compressUncompressedFile(groupCtx, listed.Path, backup)
			if err != nil {
				return err
			}
			files[index] = file

			mu.Lock()
			defer mu.Unlock()
			done++
			if progress != nil {
				progress(done, len(textures), listed.Path)
			}
			return nil
		})
	}
	err := group.Wait()

	result := TextureResizeResult{Files: make([]TextureResizeFileResult, 0, len(files))}
	for _, file := range files {
		switch file.Status {
		case "":
			continue
		case "updated":
			result.Updated++
		case "skipped":
			result.Skipped++
		default:
			result.Failed++
		}
		result.Files = append(result.Files, file)
	}
	result.Processed = len(result.Files)
	return result, err
}

// compressUncompressedFile reports a file it could not convert in the result. Its error is only
// ever the cancellation of ctx.
func compressUncompressedFile(ctx context.Context, path string, backup bool) (TextureResizeFileResult, error) {
	result, err := convertUncompressedFile(ctx, path, backup)
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		return TextureResizeFileResult{}, ctxErr
	}
	if err != nil {
		return TextureResizeFileResult{
			FilePath: path, Status: "failed",
			OriginalFormat: unknownTextureFormatName, OutputFormat: unknownTextureFormatName,
			Message: lo.ToPtr(err.Error()),
		}, nil
	}
	return result, nil
}

// convertUncompressedFile holds a disk slot only while it reads and while it writes, so the long
// encode between the two does not keep other disk work waiting. It decodes to 8-bit channels, a
// quarter of what the float pipeline of the resize tool needs for the same texture.
func convertUncompressedFile(ctx context.Context, path string, backup bool) (TextureResizeFileResult, error) {
	if err := ctx.Err(); err != nil {
		return TextureResizeFileResult{}, err
	}
	release, err := diskio.Acquire(ctx, path)
	if err != nil {
		return TextureResizeFileResult{}, err
	}
	current, dds, err := loadUncompressedTexture(path)
	release()
	if err != nil {
		return TextureResizeFileResult{}, err
	}
	if dds == nil {
		return skippedResizeFileResult(path, 0, 0, unknownTextureFormatName, unknownTextureFormatName,
			"Texture is no longer an uncompressed DDS."), nil
	}

	surface, err := ddsutil.SurfaceFromDds(dds)
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to decode DDS metadata '%s': %s", path, err)
	}
	decoded, err := surface.DecodeRgba8()
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to decode DDS pixels '%s': %s", path, err)
	}
	format := ddsutil.BC7RgbaUnorm
	if current.srgb {
		format = ddsutil.BC7RgbaUnormSrgb
	}
	// The levels are encoded as they are instead of being generated again from the base, because a
	// mod can ship levels it authored by hand.
	encoded, err := decoded.Encode(format, ddsutil.QualityFast, ddsutil.MipmapsFromSurface)
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to encode processed DDS '%s': %s", path, err)
	}
	compressed, err := encoded.ToDds()
	if err != nil {
		return TextureResizeFileResult{}, textureError("Failed to create DDS '%s': %s", path, err)
	}

	return replaceUncompressedFile(ctx, current, compressed, textureImageFormatName(format), backup)
}

// replaceUncompressedFile writes the encoded texture over the file it was read from. It leaves a
// file alone that something else rewrote during the encode, which would otherwise lose that change.
func replaceUncompressedFile(
	ctx context.Context,
	current UncompressedTexture,
	compressed *ddsutil.Dds,
	outputFormat string,
	backup bool,
) (TextureResizeFileResult, error) {
	// Waiting for a slot fails on a cancelled context, but only a rotational disk has slots, so
	// without this a cancelled run would keep its encoded files on one disk and drop them on another.
	release, err := diskio.Acquire(context.WithoutCancel(ctx), current.Path)
	if err != nil {
		return TextureResizeFileResult{}, err
	}
	defer release()
	if textureChanged(current.info, current.Path) {
		return skippedResizeFileResult(current.Path, current.Width, current.Height, current.Format, current.Format,
			"Texture changed while it was being compressed."), nil
	}

	backupCreated := false
	if backup {
		if backupCreated, err = createBackupIfMissing(current.Path); err != nil {
			return TextureResizeFileResult{}, err
		}
	}
	if err := writeDDSAtomically(current.Path, compressed); err != nil {
		return TextureResizeFileResult{}, err
	}
	return TextureResizeFileResult{
		FilePath: current.Path, Status: "updated",
		OriginalWidth: current.Width, OriginalHeight: current.Height,
		OutputWidth: current.Width, OutputHeight: current.Height,
		OriginalFormat: current.Format, OutputFormat: outputFormat,
		BackupCreated: backupCreated,
	}, nil
}

// ResizeBackupEnabled reports whether the texture tools keep a .bak copy of a file they rewrite.
func ResizeBackupEnabled(ctx context.Context, client *db.Client) (bool, error) {
	value, err := client.Settings.GetValue(ctx, textureSettingKeys.backup)
	if err != nil || value == nil || *value == "" {
		return defaultTextureSettings().Backup, err
	}
	return textureBackupEnabled(*value), nil
}

func textureBackupEnabled(raw string) bool {
	return raw == "1" || strings.EqualFold(raw, "true")
}
