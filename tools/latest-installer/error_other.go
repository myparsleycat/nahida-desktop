//go:build !windows

package main

import (
	"fmt"
	"os"
)

func showError(title string, message string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, message)
}
