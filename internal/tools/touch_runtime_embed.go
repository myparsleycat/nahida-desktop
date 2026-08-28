package tools

import "embed"

// touchRuntimeShaders keeps the original Electron runtime shader sources in the
// Go binary, so generated mods do not depend on a loose installation folder.
//
//go:embed touch_runtime/*.hlsl
var touchRuntimeShaders embed.FS
