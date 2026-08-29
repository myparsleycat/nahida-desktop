package main

import (
	"embed"
	"log"

	"nahida.live/desktop/internal/app"
	"nahida.live/desktop/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	if err := app.Run(assets, appIcon); err != nil {
		if dialogErr := platform.ShowStartupError(err); dialogErr != nil {
			log.Printf("failed to show startup error dialog: %v", dialogErr)
		}
		log.Fatal(err)
	}
}
