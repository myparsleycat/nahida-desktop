package platform

import "os"

// Packaged follows the production build tag used by the Wails build tasks.
// NAHIDA_DEV remains an explicit override for diagnostics and tests: a
// non-empty value forces development mode, while an explicitly empty value
// forces packaged mode.
func Packaged() bool {
	if dev, ok := os.LookupEnv("NAHIDA_DEV"); ok {
		return dev == ""
	}
	return packagedBuild
}
