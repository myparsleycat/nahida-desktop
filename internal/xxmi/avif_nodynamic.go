//go:build !nodynamic

package xxmi

// Every build of this package must pass the nodynamic tag. Without it github.com/gen2brain/avif loads libavif.dll
// from the DLL search path while the program initializes, before any code here can stop it. The undefined name
// fails such a build instead of producing a binary that does so.
var _ = buildRequiresNodynamicTag
