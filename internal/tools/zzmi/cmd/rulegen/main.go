package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/tools/zzmi"
)

func main() {
	source := flag.String("source", "", "path to the upstream Source Codes directory")
	output := flag.String("output", filepath.Join("internal", "tools", "zzmi", "default_rules.json.zst"), "output rule pack")
	tag := flag.String("tag", zzmi.EmbeddedTag, "upstream tag")
	commit := flag.String("commit", zzmi.EmbeddedCommit, "upstream commit")
	published := flag.String("published", "", "release publication timestamp")
	flag.Parse()
	if *source == "" {
		fmt.Fprintln(os.Stderr, "-source is required")
		os.Exit(2)
	}
	pack, err := zzmi.CompileDirectory(*source, *tag, *commit, *published)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := zzmi.EncodePack(*pack)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d hashes, %d collisions, %d bytes\n", *output, len(pack.HashCommands), pack.Collisions, len(data))
}
