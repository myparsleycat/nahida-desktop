//go:build windows

package main

import (
	"context"
	"flag"
	"os"

	"nahida.live/desktop/internal/elevated"
)

func main() {
	pipe := flag.String("pipe", "", "private named pipe")
	secret := flag.String("secret", "", "one-time session secret")
	parentPID := flag.Uint("parent-pid", 0, "Nahida Desktop process ID")
	parentExe := flag.String("parent-exe", "", "Nahida Desktop executable path")
	flag.Parse()
	if err := elevated.RunServer(context.Background(), elevated.ServerOptions{
		Pipe: *pipe, Secret: *secret, ParentPID: uint32(*parentPID), ParentExe: *parentExe,
	}); err != nil {
		os.Exit(1)
	}
}
