package xxmi

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/provenance"
)

// importerPackageSource is a repository that publishes an importer package.
type importerPackageSource struct {
	id             string
	repo           github.Repo
	assetFormat    string
	versionFile    string
	versionPattern *regexp.Regexp
	publicKey      string
	// attestation verifies releases by their build provenance instead of a signature by publicKey.
	attestation *packageAttestation
	// beyondD3D11 is whether the package applies mods to a game rendering with DirectX 12 or Vulkan.
	beyondD3D11 bool
}

type importerPackageSpec struct {
	// importerPackageSource is the source the spec resolves to. In importerPackages it is the official source, which
	// an installation without a recorded source came from.
	importerPackageSource
	key string
	// alternatives are the other sources the importer can install from.
	alternatives []importerPackageSource
	// defaultSource is the source a config selects until the user picks one; empty selects the official source.
	defaultSource string
	gameExeNames  []string
	processNames  []string
	useHook       bool
	// gameLaunches limits GameLaunch to these values, the first being the default; empty allows allGameLaunches.
	gameLaunches []string
	// nexonGameID is the game number Nexon Game Manager starts for a "Nexon" launch, and nexonExeName the image it
	// starts for that number.
	nexonGameID  string
	nexonExeName string
}

var (
	importerVersionPattern = regexp.MustCompile(`^global \$version = (\d+)\.*(\d)(\d*)`)
	wwmiVersionPattern     = regexp.MustCompile(`^global \$wwmi_version = (\d+)\.*(\d)(\d*)`)
	// forkWWMIVersionPattern reads the fork's revision of an official release from the digits after the official
	// version, which mods compare against the version they require: 1.221 is revision 1 of 1.2.2.
	forkWWMIVersionPattern = regexp.MustCompile(`^global \$wwmi_version = (\d+)\.(\d)(\d)(\d+)`)
)

var importerPackages = map[string]importerPackageSpec{
	"GIMI": {
		key: "GIMI", useHook: true,
		importerPackageSource: importerPackageSource{
			id: "silentnightsound", repo: github.Repo{Owner: "SilentNightSound", Name: "GIMI-Package"},
			assetFormat: "GIMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "GIMI", "main.ini"),
			versionPattern: importerVersionPattern, publicKey: gimiPublicKey,
		},
		gameExeNames: []string{"GenshinImpact.exe", "YuanShen.exe"},
	},
	"SRMI": {
		key: "SRMI", useHook: true,
		importerPackageSource: importerPackageSource{
			id: "spectrumqt", repo: github.Repo{Owner: "SpectrumQT", Name: "SRMI-Package"},
			assetFormat: "SRMI-TEST-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "SRMI", "main.ini"),
			versionPattern: importerVersionPattern, publicKey: spectrumPublicKey,
		},
		gameExeNames: []string{"StarRail.exe"},
	},
	"WWMI": {
		key: "WWMI",
		importerPackageSource: importerPackageSource{
			id: "spectrumqt", repo: github.Repo{Owner: "SpectrumQT", Name: "WWMI-Package"},
			assetFormat: "WWMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "WWMI", "WuWa-Model-Importer.ini"),
			versionPattern: wwmiVersionPattern, publicKey: spectrumPublicKey,
		},
		alternatives: []importerPackageSource{{
			id: "myparsleycat", repo: github.Repo{Owner: "myparsleycat", Name: "WWMI-Package"},
			assetFormat: "WWMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "WWMI", "WuWa-Model-Importer.ini"),
			versionPattern: forkWWMIVersionPattern, beyondD3D11: true,
			attestation: &packageAttestation{
				build: provenance.Build{
					Repository: "https://github.com/myparsleycat/WWMI-Package",
					Workflow:   ".github/workflows/draft-release.yml",
					Refs:       []string{"refs/heads/main"},
				},
				bundleFormat:   "WWMI-PACKAGE-v%s.sigstore.json",
				versionSubject: "wwmi-package-release-version",
			},
		}},
		defaultSource: "myparsleycat",
		gameExeNames:  []string{"Wuthering Waves.exe"},
		processNames:  []string{"Client-Win64-Shipping.exe"},
	},
	"ZZMI": {
		key: "ZZMI", useHook: true,
		importerPackageSource: importerPackageSource{
			id: "leotorrez", repo: github.Repo{Owner: "leotorrez", Name: "ZZMI-Package"},
			assetFormat: "ZZMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "ZZMI", "main.ini"),
			versionPattern: importerVersionPattern, publicKey: zzmiPublicKey,
		},
		gameExeNames: []string{"ZenlessZoneZero.exe", "ZenlessZoneZeroBeta.exe"},
	},
	"HIMI": {
		key: "HIMI", useHook: true,
		importerPackageSource: importerPackageSource{
			id: "leotorrez", repo: github.Repo{Owner: "leotorrez", Name: "HIMI-Package"},
			assetFormat: "HIMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "HIMI", "main.ini"),
			versionPattern: importerVersionPattern, publicKey: himiPublicKey,
		},
		gameExeNames: []string{"BH3.exe"},
	},
	"EFMI": {
		key: "EFMI",
		importerPackageSource: importerPackageSource{
			id: "spectrumqt", repo: github.Repo{Owner: "SpectrumQT", Name: "EFMI-Package"},
			assetFormat: "EFMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "EFMI", "main.ini"),
			versionPattern: importerVersionPattern, publicKey: spectrumPublicKey,
		},
		gameExeNames: []string{"Endfield.exe"},
	},
	"APMI": {
		key: "APMI",
		importerPackageSource: importerPackageSource{
			id: "myparsleycat", repo: github.Repo{Owner: "myparsleycat", Name: "APMI-Package"},
			assetFormat: "APMI-PACKAGE-v%s.zip", versionFile: filepath.Join("Core", "APMI", "main.ini"),
			versionPattern: importerVersionPattern,
			attestation: &packageAttestation{
				build: provenance.Build{
					Repository: "https://github.com/myparsleycat/APMI-Package",
					Workflow:   ".github/workflows/draft-release.yml",
					Refs:       []string{"refs/heads/main"},
				},
				bundleFormat:   "APMI-PACKAGE-v%s.sigstore.json",
				versionSubject: "apmi-package-release-version",
			},
		},
		gameExeNames: []string{"AzurPromilia.exe", "AzurPromiliaJP.exe"},
		gameLaunches: []string{"Manual", "Nexon"},
		nexonGameID:  "2887",
		nexonExeName: "AzurPromilia.exe",
	},
}

var allGameLaunches = []string{"Direct", "Steam", "Epic", "Custom", "Manual"}

func lookupImporterPackage(key string) (importerPackageSpec, bool) {
	spec, ok := importerPackages[strings.ToUpper(strings.TrimSpace(key))]
	return spec, ok
}

// sources lists the importer's package sources, the official one first.
func (spec importerPackageSpec) sources() []importerPackageSource {
	official := importerPackages[spec.key].importerPackageSource
	return append([]importerPackageSource{official}, spec.alternatives...)
}

func (spec importerPackageSpec) withSource(id string) (importerPackageSpec, bool) {
	for _, source := range spec.sources() {
		if source.id == id {
			spec.importerPackageSource = source
			return spec, true
		}
	}
	return importerPackageSpec{}, false
}

// resolveImporterPackage returns the importer's spec resolved to the package source cfg installs from.
func resolveImporterPackage(key string, cfg ImporterConfig) (importerPackageSpec, bool) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return importerPackageSpec{}, false
	}
	id, _ := PackageSource(spec.key, cfg)
	return spec.withSource(id)
}

// PackageSources lists the package sources the importer can install from, the official one first. An importer
// with fewer than two has nothing to choose.
func PackageSources(key string) []string {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return nil
	}
	sources := spec.sources()
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.id)
	}
	return ids
}

// PackageSource returns the package source the importer installs from under cfg. forced reports that the renderer
// leaves no choice: a game that may render with DirectX 12 or Vulkan needs a package that applies mods there when
// the official package does not.
func PackageSource(key string, cfg ImporterConfig) (id string, forced bool) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return "", false
	}
	sources := spec.sources()
	if rendererBeyondD3D11(spec.key, cfg) && !sources[0].beyondD3D11 {
		for _, source := range sources {
			if source.beyondD3D11 {
				return source.id, true
			}
		}
	}
	if slices.ContainsFunc(sources, func(source importerPackageSource) bool { return source.id == cfg.PackageSource }) {
		return cfg.PackageSource, false
	}
	if spec.defaultSource != "" {
		return spec.defaultSource, false
	}
	return sources[0].id, false
}

// ImporterPackageID names the importer's package from a source in update records and release lists. The official
// source keeps the name it had before importers had other sources.
func ImporterPackageID(key, source string) string {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return "importer:" + key
	}
	if source == "" || source == spec.id {
		return "importer:" + spec.key
	}
	return "importer:" + spec.key + "@" + source
}

// SplitImporterPackageID returns an ImporterPackageID without the package source it names and that source, which
// is empty for the official source and for other packages.
func SplitImporterPackageID(pkg string) (base, source string) {
	if !strings.HasPrefix(pkg, "importer:") {
		return pkg, ""
	}
	base, source, _ = strings.Cut(pkg, "@")
	return base, source
}

// parseImporterPackageID returns the spec resolved to the source an ImporterPackageID names.
func parseImporterPackageID(pkg string) (importerPackageSpec, bool) {
	name, ok := strings.CutPrefix(pkg, "importer:")
	if !ok {
		return importerPackageSpec{}, false
	}
	key, source, named := strings.Cut(name, "@")
	spec, ok := lookupImporterPackage(key)
	switch {
	case !ok:
		return importerPackageSpec{}, false
	case !named:
		return spec, true
	case source == spec.id:
		return importerPackageSpec{}, false
	}
	return spec.withSource(source)
}

// installedImporterPackage returns the version and source of the package in folder. The source is the one the
// installation recorded for the version the folder holds. Without that record, it is a source whose own version
// format the version file uses, or the official source, as for an installation made before importers had other
// sources or by the external launcher.
func installedImporterPackage(folder string, spec importerPackageSpec) (version *string, source string) {
	official, _ := spec.withSource(spec.sources()[0].id)
	record, err := readPackageVerification(folder)
	if err == nil && record != nil {
		if recorded, ok := spec.withSource(cmp.Or(record.Source, official.id)); ok {
			version := readImporterVersion(folder, recorded)
			if version != nil && normalizeVersion(*version) == normalizeVersion(record.Version) {
				return version, recorded.id
			}
		}
	}

	// The official pattern also reads the fork's WWMI version 1.221, as 1.2.21, but no official release has a
	// patch number above 9, so a value the fork's pattern reads is the fork's.
	for _, alternative := range spec.alternatives {
		if alternative.versionPattern == official.versionPattern {
			continue
		}
		resolved, _ := spec.withSource(alternative.id)
		if version := readImporterVersion(folder, resolved); version != nil {
			return version, alternative.id
		}
	}
	return readImporterVersion(folder, official), official.id
}

// selectedInstalledPackage returns the version of the package in the importer folder, or nil when the folder holds
// none or one from another source than cfg selects.
func selectedInstalledPackage(key string, cfg ImporterConfig) *string {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return nil
	}
	installed, source := installedImporterPackage(cfg.ImporterFolder, spec)
	if selected, _ := PackageSource(spec.key, cfg); source != selected {
		return nil
	}
	return installed
}

// GameLaunches returns the GameLaunch values the importer accepts.
func GameLaunches(key string) []string {
	if spec, ok := lookupImporterPackage(key); ok && len(spec.gameLaunches) > 0 {
		return slices.Clone(spec.gameLaunches)
	}
	return allGameLaunches
}

func readImporterVersion(folder string, spec importerPackageSpec) *string {
	version, err := parseImporterVersionFile(filepath.Join(folder, spec.versionFile), spec.versionPattern)
	if err != nil || version == "" {
		return nil
	}
	return &version
}

func validateInstalledImporterPackage(key string, cfg ImporterConfig) error {
	if cfg.PackageVersion.Pinned == "" {
		return nil
	}
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return fmt.Errorf("unknown importer %q", key)
	}
	installed, source := installedImporterPackage(cfg.ImporterFolder, spec)
	if installed == nil {
		return fmt.Errorf(
			"XXMI_IMPORTER_NOT_INSTALLED: selected package %s is not installed",
			cfg.PackageVersion.Pinned,
		)
	}

	// A version number names a release only within one source, and two sources may publish the same one.
	if selected, _ := PackageSource(spec.key, cfg); source != selected {
		return fmt.Errorf(
			"XXMI_IMPORTER_NOT_INSTALLED: selected package %s from %s differs from installed package %s from %s",
			cfg.PackageVersion.Pinned,
			selected,
			*installed,
			source,
		)
	}
	if normalizeVersion(*installed) != normalizeVersion(cfg.PackageVersion.Pinned) {
		return fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: selected package %s differs from installed package %s",
			cfg.PackageVersion.Pinned, *installed)
	}
	return nil
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
		switch len(matches) {
		case 4:
			patch := matches[3]
			if patch == "" {
				patch = "0"
			}
			return matches[1] + "." + matches[2] + "." + patch, nil
		case 5:
			return strings.Join(matches[1:], "."), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}
