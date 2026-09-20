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
	flag.Parse()
	if err := elevated.RunServer(context.Background(), elevated.ServerOptions{
		Pipe: *pipe, Secret: *secret, ParentPID: uint32(*parentPID),
	}); err != nil {
		os.Exit(1)
	}
}
