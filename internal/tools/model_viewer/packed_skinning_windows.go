package modelviewer

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/cpu"

	"nahida.live/desktop/internal/nativecompute"
)

func lookupPackedSkinningFrame(library *nativecompute.Library) (uintptr, error) {
	if !cpu.X86.HasAVX || !cpu.X86.HasF16C {
		return 0, nil
	}
	supports, err := library.Lookup("packed_skinning_supported")
	if err != nil || supports == 0 {
		return 0, err
	}
	supported, _, _ := syscall.SyscallN(supports)
	if uint32(supported) == 0 {
		return 0, nil
	}
	return library.Lookup("packed_skinning_frame")
}

// packedSkinningFrameArgs follows the C layout of FrameArgs in native/desktop_kernels/src/packed_skinning.rs.
type packedSkinningFrameArgs struct {
	base, weights      *float32
	bones              *int32
	palette0, palette1 *float32
	positions, normals *float32
	vertexCount        uintptr
	boneCount          uintptr
	fraction           float64
}

func runPackedSkinningFrame(proc uintptr, args packedSkinningFrameArgs) error {
	// The DLL borrows all seven Go buffers until this synchronous call returns.
	var pins runtime.Pinner
	pins.Pin(args.base)
	pins.Pin(args.weights)
	pins.Pin(args.bones)
	pins.Pin(args.palette0)
	pins.Pin(args.palette1)
	pins.Pin(args.positions)
	pins.Pin(args.normals)
	defer pins.Unpin()

	result, _, _ := syscall.SyscallN(proc, uintptr(unsafe.Pointer(&args)))
	switch code := int(result); {
	case code == 0:
		return nil
	case code == -2:
		return errors.New("packed dual-quaternion position exceeds finite float16 range")
	case code > 0:
		return fmt.Errorf("invalid packed dual-quaternion pose at vertex %d", code-1)
	default:
		return fmt.Errorf("packed skinning returned status %d", code)
	}
}
