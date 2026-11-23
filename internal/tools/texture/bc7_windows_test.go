//go:build native_kernels && amd64

package texture

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/myparsleycat/ddsutil"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sys/cpu"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/nativecompute"
)

func TestBC7BundledKernelMatchesGo(t *testing.T) {
	t.Parallel()
	const cacheEnv = "NAHIDA_TEST_BC7_CACHE"
	cache := os.Getenv(cacheEnv)
	if cache == "" {
		// Windows keeps the loaded DLL file locked until the subprocess exits.
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestBC7BundledKernelMatchesGo$")
		cmd.Env = append(cmd.Environ(), cacheEnv+"="+t.TempDir())
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("BC7 subprocess: %v\n%s", err, output)
		}
		return
	}
	store, err := appdata.Open(cache)
	if err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseNativeCompute(nativecompute.New(store, nil))
	encode := service.bc7Encoder(t.Context())

	const width, height = 517, 513
	const paddedWidth, paddedHeight = 520, 516
	rgba := make([]byte, width*height*4)
	padded := make([]byte, paddedWidth*paddedHeight*4)
	rng := rand.New(rand.NewPCG(11, 71))
	for i := 16; i < len(rgba); i++ {
		rgba[i] = uint8(rng.Uint32())
	}
	colorBlock := bc7ColorRegressionBlock()
	alphaBlock := bc7AlphaRegressionBlock()
	isolatedColorBlock := append(
		bytes.Repeat([]byte{72, 144, 72, 255}, 8),
		bytes.Repeat([]byte{144, 72, 144, 255}, 6)...)
	isolatedColorBlock = append(isolatedColorBlock, 0, 0, 144, 255, 0, 0, 144, 255)
	for row := range 4 {
		copy(rgba[row*width*4:], colorBlock[row*16:][:16])
		copy(rgba[row*width*4+16:], alphaBlock[row*16:][:16])
		copy(rgba[row*width*4+32:], isolatedColorBlock[row*16:][:16])
	}
	for row := range height {
		copy(padded[row*paddedWidth*4:], rgba[row*width*4:][:width*4])
	}
	source := &ddsutil.SurfaceRgba8{Width: width, Height: height, Depth: 1, Layers: 1, Mipmaps: 1, Data: rgba}
	want, err := source.Encode(ddsutil.BC7RgbaUnormSrgb, ddsutil.QualityFast, ddsutil.MipmapsDisabled)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	got, err := encodeTexture(t.Context(), source, ddsutil.BC7RgbaUnormSrgb, ddsutil.MipmapsDisabled,
		func(width, height uint32, rgba, dst []byte) (bool, error) {
			calls++
			if width != paddedWidth || height != paddedHeight || !bytes.Equal(rgba, padded) {
				t.Fatalf("callback received unexpected padded RGBA8: %dx%d, %d bytes", width, height, len(rgba))
			}
			handled, err := encode(width, height, rgba, dst)
			if err == nil && handled != (cpu.X86.HasAVX && cpu.X86.HasAVX2) {
				t.Fatalf("native BC7 handled = %t", handled)
			}
			return handled, err
		})
	if err != nil || calls != 1 {
		t.Fatalf("texture encode used the BC7 callback %d times, error = %v", calls, err)
	}
	decoded := assertBC7AlphaCoverage(t, got, rgba)
	assertBC7DominantColor(t, decoded, width)
	if !bytes.Equal(got.Data, want.Data) {
		t.Fatal("texture encode differs from the Go BC7 output")
	}

	dst := make([]byte, paddedWidth*paddedHeight)
	if cpu.X86.HasAVX && cpu.X86.HasAVX2 {
		if _, err := encode(paddedWidth, paddedHeight, padded, dst[:len(dst)-1]); err == nil {
			t.Fatal("accepted an undersized output buffer")
		}
	}
	if handled, err := encode(4, 4, make([]byte, 64), make([]byte, 16)); handled || err != nil {
		t.Fatalf("small mip handled = %t, error = %v", handled, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if handled, err := service.bc7Encoder(
		ctx,
	)(
		paddedWidth,
		paddedHeight,
		padded,
		dst,
	); handled ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled encode handled = %t, error = %v", handled, err)
	}

	t.Run("launch compression respects cancellation before the first slot", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "Diffuse.dds")
		writeTestDDS(t, path, 1024, 1024, ddsutil.Rgba8Unorm)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var calls atomic.Int32
		kernel := &nativeBC7{workers: 1, slots: semaphore.NewWeighted(1)}
		kernel.proc = syscall.NewCallback(func(_, _, _, _ uintptr) uintptr {
			calls.Add(1)
			return 1
		})
		if err := kernel.slots.Acquire(ctx, 1); err != nil {
			t.Fatal(err)
		}
		launchService := New()
		launchService.loadBC7 = func() *nativeBC7 {
			cancel()
			kernel.slots.Release(1)
			return kernel
		}
		result, err := launchService.CompressUncompressed(ctx, []UncompressedTexture{{Path: path}}, true, nil)
		if !errors.Is(err, context.Canceled) || len(result.Files) != 0 || calls.Load() != 0 {
			t.Fatalf("cancelled texture compression = %+v, error = %v, native calls = %d", result, err, calls.Load())
		}
		if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, original) {
			t.Fatalf("cancelled texture changed, read error = %v", err)
		}
		if regularFile(path + ".bak") {
			t.Fatal("encoding never started but a backup was created")
		}
	})

	t.Run("launch compression saves the active texture on cancellation", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "Diffuse.dds")
		source := &ddsutil.SurfaceRgba8{
			Width: 1024, Height: 1024, Depth: 1, Layers: 1, Mipmaps: 1,
			Data: bytes.Repeat([]byte{64, 128, 192, 255}, 1024*1024),
		}
		dds, err := source.EncodeDds(ddsutil.Rgba8Unorm, ddsutil.QualityFast, ddsutil.MipmapsGeneratedExact(2))
		if err != nil {
			t.Fatal(err)
		}
		if err := writeDDSAtomically(path, dds); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		launchService := New()
		kernel := service.loadBC7()
		if kernel != nil {
			active := *kernel
			active.proc = syscall.NewCallback(func(src, dst, width, rows uintptr) uintptr {
				cancel()
				result, _, _ := syscall.SyscallN(kernel.proc, src, dst, width, rows)
				return result
			})
			launchService.loadBC7 = func() *nativeBC7 { return &active }
		} else {
			launchService.loadBC7 = func() *nativeBC7 { return nil }
		}
		result, err := compressUncompressed(ctx, []UncompressedTexture{{Path: path}}, true, nil,
			func(ctx context.Context) bc7FastEncoder {
				encode := launchService.newBC7Encoder(ctx, true)
				if kernel != nil {
					return encode
				}
				return func(width, height uint32, rgba, dst []byte) (bool, error) {
					handled, err := encode(width, height, rgba, dst)
					cancel()
					return handled, err
				}
			})
		if err != nil || ctx.Err() == nil || result.Updated != 1 || len(result.Files) != 1 ||
			!result.Files[0].BackupCreated {
			t.Fatalf("active texture compression = %+v, error = %v, context error = %v", result, err, ctx.Err())
		}
		encoded, err := readDDSFile(path)
		if err != nil || encoded.NumMipmapLevels() != 2 {
			t.Fatalf("expected both authored mip levels to be saved, read error = %v", err)
		}
	})
}
