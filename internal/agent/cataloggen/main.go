// Command cataloggen regenerates internal/agent/providers/catalog.json from models.dev.
//
// Run it from the repository root after the provider list or the projection rules change:
//
//	go run ./internal/agent/cataloggen
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nahida.live/desktop/internal/agent"
)

const (
	defaultSource = "https://models.dev/api.json"
	defaultOut    = "internal/agent/providers/catalog.json"
)

func main() {
	source := flag.String("source", defaultSource, "models.dev catalog URL")
	out := flag.String("out", defaultOut, "catalog file to write")
	flag.Parse()

	if err := run(*source, *out); err != nil {
		fmt.Fprintln(os.Stderr, "cataloggen:", err)
		os.Exit(1)
	}
}

func run(source, out string) error {
	raw, err := fetch(source)
	if err != nil {
		return err
	}

	catalog, err := agent.ProjectCatalog(raw, source, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}

	encoded, err := agent.EncodeCatalog(catalog)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, encoded, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	for _, provider := range catalog.Providers {
		fmt.Printf("%-14s %d models\n", provider.ID, len(provider.Models))
	}
	return nil
}

func fetch(source string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 2 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", source, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected status %d", source, response.StatusCode)
	}
	return io.ReadAll(response.Body)
}
