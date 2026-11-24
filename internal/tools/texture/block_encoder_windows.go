package texture

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/ddsutil"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sys/cpu"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/nativecompute"
)

type blockEncoder = ddsutil.BlockEncoder

// nativeBlocks holds the native block encoders that are available. A zero proc is unavailable.
type nativeBlocks struct {
	bc7     uintptr
	bcn     uintptr
	workers int
	slots   *semaphore.Weighted
}

type bcnFormat struct {
	code       uintptr
	blockBytes int
}

// bcnFormats follows the format codes and block sizes of bcn_encode_rows in native/desktop_kernels/src/bcn.rs.
var bcnFormats = map[ddsutil.ImageFormat]bcnFormat{
	ddsutil.BC1RgbaUnorm: {1, 8}, ddsutil.BC1RgbaUnormSrgb: {1, 8},
	ddsutil.BC2RgbaUnorm: {2, 16}, ddsutil.BC2RgbaUnormSrgb: {2, 16},
	ddsutil.BC3RgbaUnorm: {3, 16}, ddsutil.BC3RgbaUnormSrgb: {3, 16},
	ddsutil.BC4RUnorm: {4, 8}, ddsutil.BC4RSnorm: {5, 8},
	ddsutil.BC5RgUnorm: {6, 16}, ddsutil.BC5RgSnorm: {7, 16},
}

func (t *Service) UseNativeCompute(library *nativecompute.Library) {
	t.loadNative = sync.OnceValue(func() *nativeBlocks {
		bc7, bc7Err := lookupBlockEncoder(library, "bc7_fast_supported", "bc7_fast_encode_rows")
		bcn, bcnErr := lookupBlockEncoder(library, "bcn_supported", "bcn_encode_rows")
		if err := errors.Join(bc7Err, bcnErr); err != nil {
			_ = infra.ReportError(t.log, err, "Texture.encodeBlocks", infra.Diagnostic{
				Operation: "bind-block-encoders", Severity: infra.DiagnosticWarn,
			})
		}
		if bc7 == 0 && bcn == 0 {
			return nil
		}
		workers := runtime.GOMAXPROCS(0)
		return &nativeBlocks{bc7: bc7, bcn: bcn, workers: workers, slots: semaphore.NewWeighted(int64(workers))}
	})
}

func lookupBlockEncoder(library *nativecompute.Library, supportedSymbol, encodeSymbol string) (uintptr, error) {
	if !cpu.X86.HasAVX || !cpu.X86.HasAVX2 {
		return 0, nil
	}
	supports, err := library.Lookup(supportedSymbol)
	if err != nil || supports == 0 {
		return 0, err
	}
	supported, _, _ := syscall.SyscallN(supports)
	if uint32(supported) == 0 {
		return 0, nil
	}
	return library.Lookup(encodeSymbol)
}

func (t *Service) nativeEncoder(ctx context.Context) blockEncoder {
	return t.newNativeEncoder(ctx, false)
}

func (t *Service) newNativeEncoder(ctx context.Context, finishFile bool) blockEncoder {
	if t.loadNative == nil {
		return nil
	}
	var start func() context.Context
	if finishFile {
		// Each launch compression callback owns one file. Its remaining mips share this context.
		start = func() context.Context {
			ctx = context.WithoutCancel(ctx)
			start = nil
			return ctx
		}
	}
	return func(format ddsutil.ImageFormat, quality ddsutil.Quality, width, height uint32, data, dst []byte) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var kernel *nativeBlocks
		if uint64(width)*uint64(height) >= 512*512 {
			kernel = t.loadNative()
		}
		if kernel != nil {
			if rows, blockBytes := kernel.rows(format, quality); rows != nil {
				return true, kernel.encode(ctx, width, height, data, dst, blockBytes, rows, start)
			}
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if start != nil {
			start()
		}
		return false, nil
	}
}

// rows returns the call that encodes block rows of format at quality, and the size of its blocks, or nil if
// no native encoder handles them.
func (k *nativeBlocks) rows(
	format ddsutil.ImageFormat,
	quality ddsutil.Quality,
) (func(src, dst unsafe.Pointer, width uint32, rows int) uintptr, int) {
	if bcn, ok := bcnFormats[format]; ok && k.bcn != 0 {
		return func(src, dst unsafe.Pointer, width uint32, rows int) uintptr {
			result, _, _ := syscall.SyscallN(k.bcn, bcn.code, uintptr(quality), uintptr(src), uintptr(dst),
				uintptr(width), uintptr(rows))
			return result
		}, bcn.blockBytes
	}
	if (format == ddsutil.BC7RgbaUnorm || format == ddsutil.BC7RgbaUnormSrgb) && quality == ddsutil.QualityFast &&
		k.bc7 != 0 {
		return func(src, dst unsafe.Pointer, width uint32, rows int) uintptr {
			result, _, _ := syscall.SyscallN(k.bc7, uintptr(src), uintptr(dst), uintptr(width), uintptr(rows))
			return result
		}, 16
	}
	return nil, 0
}

func (k *nativeBlocks) encode(
	ctx context.Context,
	width, height uint32,
	rgba, dst []byte,
	blockBytes int,
	encodeRows func(src, dst unsafe.Pointer, width uint32, rows int) uintptr,
	start func() context.Context,
) error {
	pixels := uint64(width) * uint64(height)
	if pixels == 0 || width%4 != 0 || height%4 != 0 ||
		pixels/16*uint64(blockBytes) != uint64(len(dst)) || pixels != uint64(len(rgba))/4 || len(rgba)%4 != 0 {
		return errors.New("invalid block buffer dimensions")
	}
	if err := k.slots.Acquire(ctx, 1); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		k.slots.Release(1)
		return err
	}
	if start != nil {
		ctx = start()
	}

	// The DLL borrows disjoint rows of these buffers until the workers have returned.
	var pins runtime.Pinner
	pins.Pin(&rgba[0])
	pins.Pin(&dst[0])
	defer pins.Unpin()

	// Rows are handed out as workers finish, because flat and detailed regions cost very different times.
	const batch = 8
	rows := int(height / 4)
	inputStride, outputStride := int(width)*16, int(width/4)*blockBytes
	workers := min(k.workers, (rows+batch-1)/batch)
	var next atomic.Int64
	group, groupCtx := errgroup.WithContext(ctx)
	for worker := range workers {
		group.Go(func() error {
			if worker != 0 {
				if err := k.slots.Acquire(groupCtx, 1); err != nil {
					return err
				}
			}
			defer k.slots.Release(1)
			for {
				row := int(next.Add(batch) - batch)
				if row >= rows {
					return nil
				}
				if err := groupCtx.Err(); err != nil {
					return err
				}
				if encodeRows(unsafe.Pointer(&rgba[row*inputStride]), unsafe.Pointer(&dst[row*outputStride]), width,
					min(batch, rows-row)) != 0 {
					return errors.New("native block encoding is unavailable")
				}
			}
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}
