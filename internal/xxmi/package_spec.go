package xxmi

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type importerPackageSpec struct {
	key            string
	owner          string
	repo           string
	assetFormat    string
	versionFile    string
	versionPattern *regexp.Regexp
}

var (
	importerVersionPattern = regexp.MustCompile(`^global \$version = (\d+)\.*(\d)(\d*)`)
	wwmiVersionPattern     = regexp.MustCompile(`^global \$wwmi_version = (\d+)\.*(\d)(\d*)`)
)

var importerPackages = map[string]importerPackageSpec{
	"GIMI": {
		key: "GIMI", owner: "SilentNightSound", repo: "GIMI-Package",
		assetFormat: "GIMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "GIMI", "main.ini"),
		versionPattern: importerVersionPattern,
	},
	"SRMI": {
		key: "SRMI", owner: "SpectrumQT", repo: "SRMI-Package",
		assetFormat: "SRMI-TEST-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "SRMI", "main.ini"),
		versionPattern: importerVersionPattern,
	},
	"WWMI": {
		key: "WWMI", owner: "SpectrumQT", repo: "WWMI-Package",
		assetFormat:    "WWMI-PACKAGE-v%s.zip",
		versionFile:    filepath.Join("Core", "WWMI", "WuWa-Model-Importer.ini"),
		versionPattern: wwmiVersionPattern,
	},
	"ZZMI": {
		key: "ZZMI", owner: "leotorrez", repo: "ZZMI-Package",
		assetFormat: "ZZMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "ZZMI", "main.ini"),
		versionPattern: importerVersionPattern,
	},
	"HIMI": {
		key: "HIMI", owner: "leotorrez", repo: "HIMI-Package",
		assetFormat: "HIMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "HIMI", "main.ini"),
		versionPattern: importerVersionPattern,
	},
	"EFMI": {
		key: "EFMI", owner: "SpectrumQT", repo: "EFMI-Package",
		assetFormat: "EFMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "EFMI", "main.ini"),
		versionPattern: importerVersionPattern,
	},
}

func lookupImporterPackage(key string) (importerPackageSpec, bool) {
	spec, ok := importerPackages[strings.ToUpper(strings.TrimSpace(key))]
	return spec, ok
}

func readImporterVersion(folder string, spec importerPackageSpec) *string {
	version, err := parseImporterVersionFile(filepath.Join(folder, spec.versionFile), spec.versionPattern)
	if err != nil || version == "" {
		return nil
	}
	return &version
}

func parseImporterVersionFile(path string, pattern *regexp.Regexp) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		matches := pattern.FindStringSubmatch(scanner.Text())
		if len(matches) != 4 {
			continue
		}
		patch := matches[3]
		if patch == "" {
			patch = "0"
		}
		return matches[1] + "." + matches[2] + "." + patch, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}
