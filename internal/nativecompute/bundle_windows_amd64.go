//go:build native_kernels

package nativecompute

import _ "embed"

//go:embed desktop_kernels.dll
var bundledImage []byte
