package texture

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/myparsleycat/ddsutil"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sys/cpu"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/nativecompute"
)

type bc7FastEncoder = ddsutil.BC7FastEncoder

type nativeBC7 struct {
	proc    uintptr
	workers int
	slots   *semaphore.Weighted
}

func (t *Service) UseNativeCompute(library *nativecompute.Library) {
	t.loadBC7 = sync.OnceValue(func() *nativeBC7 {
		proc, err := lookupBC7Fast(library)
		if err != nil {
			_ = infra.ReportError(t.log, err, "Texture.encodeBC7", infra.Diagnostic{
				Operation: "bind-bc7-fast", Severity: infra.DiagnosticWarn,
			})
		}
		if proc == 0 {
			return nil
		}
		workers := runtime.GOMAXPROCS(0)
		return &nativeBC7{proc: proc, workers: workers, slots: semaphore.NewWeighted(int64(workers))}
	})
}

func lookupBC7Fast(library *nativecompute.Library) (uintptr, error) {
	if !cpu.X86.HasAVX || !cpu.X86.HasAVX2 {
		return 0, nil
	}
	supports, err := library.Lookup("bc7_fast_supported")
	if err != nil || supports == 0 {
		return 0, err
	}
	supported, _, _ := syscall.SyscallN(supports)
	if uint32(supported) == 0 {
		return 0, nil
	}
	return library.Lookup("bc7_fast_encode_rows")
}

func (t *Service) bc7Encoder(ctx context.Context) bc7FastEncoder {
	return t.newBC7Encoder(ctx, false)
}

func (t *Service) newBC7Encoder(ctx context.Context, finishFile bool) bc7FastEncoder {
	if t.loadBC7 == nil {
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
	return func(width, height uint32, rgba, dst []byte) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var kernel *nativeBC7
		if uint64(width)*uint64(height) >= 512*512 {
			kernel = t.loadBC7()
		}
		if kernel != nil {
			return true, kernel.encode(ctx, width, height, rgba, dst, start)
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

func (k *nativeBC7) encode(
	ctx context.Context,
	width, height uint32,
	rgba, dst []byte,
	start func() context.Context,
) error {
	pixels := uint64(width) * uint64(height)
	if pixels == 0 || width%4 != 0 || height%4 != 0 ||
		pixels != uint64(len(dst)) || pixels != uint64(len(rgba))/4 || len(rgba)%4 != 0 {
		return errors.New("invalid BC7 block buffer dimensions")
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

	const batch = 8
	rows := int(height / 4)
	inputStride, outputStride := int(width)*16, int(width)*4
	workers := min(k.workers, (rows+batch-1)/batch)
	group, groupCtx := errgroup.WithContext(ctx)
	for worker := range workers {
		group.Go(func() error {
			if worker != 0 {
				if err := k.slots.Acquire(groupCtx, 1); err != nil {
					return err
				}
			}
			defer k.slots.Release(1)
			for row := worker * batch; row < rows; row += workers * batch {
				if err := groupCtx.Err(); err != nil {
					return err
				}
				result, _, _ := syscall.SyscallN(k.proc, uintptr(unsafe.Pointer(&rgba[row*inputStride])),
					uintptr(unsafe.Pointer(&dst[row*outputStride])), uintptr(width), uintptr(min(batch, rows-row)))
				if uint32(result) != 0 {
					return errors.New("native BC7 encoding is unavailable")
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}
