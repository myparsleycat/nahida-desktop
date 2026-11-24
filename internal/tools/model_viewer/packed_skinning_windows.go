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

type packedSkinningVariant uintptr

const (
	packedSkinningLegacy packedSkinningVariant = iota
	packedSkinningObject
	packedSkinningObjectDirect
	packedSkinningEFMITyped
	packedSkinningEFMICompact
)

type packedSkinningProcs struct {
	legacy, variants, efmi uintptr
}

func lookupPackedSkinningFrames(library *nativecompute.Library) (packedSkinningProcs, error) {
	if !cpu.X86.HasAVX || !cpu.X86.HasF16C {
		return packedSkinningProcs{}, nil
	}
	supports, err := library.Lookup("packed_skinning_supported")
	if err != nil || supports == 0 {
		return packedSkinningProcs{}, err
	}
	supported, _, _ := syscall.SyscallN(supports)
	if uint32(supported) == 0 {
		return packedSkinningProcs{}, nil
	}
	legacy, legacyErr := library.Lookup("packed_skinning_frame")
	variants, variantsErr := library.Lookup("packed_skinning_variant_frame")
	efmi, efmiErr := library.Lookup("packed_skinning_efmi_frame")
	return packedSkinningProcs{
		legacy:   legacy,
		variants: variants,
		efmi:     efmi,
	}, errors.Join(
		legacyErr,
		variantsErr,
		efmiErr,
	)
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

type packedSkinningShape struct {
	target *float32
	weight float64
}

func runPackedSkinningFrame(
	proc uintptr,
	variant packedSkinningVariant,
	args packedSkinningFrameArgs,
	shapes []packedSkinningShape,
) error {
	var pins runtime.Pinner
	// The address of args must survive Go callback reentry, including a stack move.
	pins.Pin(&args)
	pins.Pin(args.base)
	pins.Pin(args.weights)
	pins.Pin(args.bones)
	pins.Pin(args.palette0)
	pins.Pin(args.palette1)
	pins.Pin(args.positions)
	pins.Pin(args.normals)
	if len(shapes) != 0 {
		pins.Pin(&shapes[0])
		for _, shape := range shapes {
			pins.Pin(shape.target)
		}
	}
	defer pins.Unpin()

	var result uintptr
	switch variant {
	case packedSkinningLegacy:
		result, _, _ = syscall.SyscallN(proc, uintptr(unsafe.Pointer(&args)))
	case packedSkinningEFMITyped, packedSkinningEFMICompact:
		result, _, _ = syscall.SyscallN(
			proc,
			uintptr(unsafe.Pointer(&args)),
			uintptr(variant),
			uintptr(unsafe.Pointer(unsafe.SliceData(shapes))),
			uintptr(len(shapes)),
		)
	default:
		result, _, _ = syscall.SyscallN(proc, uintptr(unsafe.Pointer(&args)), uintptr(variant))
	}
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
