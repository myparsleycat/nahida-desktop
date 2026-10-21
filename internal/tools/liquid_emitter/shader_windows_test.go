//go:build windows

package liquidemitter

import (
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"math"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

// shaderReference is one shader an INI of the bundle runs, at its installed
// path, with the stage the INI key gives it.
type shaderReference struct{ file, stage string }

func bundleShaderReferences(t *testing.T, files fs.FS) []shaderReference {
	t.Helper()
	var references []shaderReference
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Ext(name) != ".ini" {
			return err
		}
		ini, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		installedDir := strings.TrimPrefix(strings.TrimPrefix(path.Dir(name), runtimeEnhancedDir), "/")
		for _, line := range strings.Split(string(ini), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if _, stage := shaderStages[key]; !ok || !stage || !strings.HasSuffix(value, ".hlsl") {
				continue
			}
			reference := shaderReference{file: path.Join(installedDir, value), stage: key}
			if !slices.Contains(references, reference) {
				references = append(references, reference)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return references
}

// Every shader the runtime INIs of each game run must compile for its stage
// with the macros the game defines, or the pass fails only as an overlay
// error inside the game.
func TestRuntimeShadersCompile(t *testing.T) {
	type job struct {
		bundle    *shaderBundle
		reference shaderReference
	}
	var jobs []job
	seen := map[[32]byte]bool{}
	for _, g := range games {
		files, err := runtimeFiles(g.dir)
		if err != nil {
			t.Fatal(err)
		}
		bundle := newShaderBundle(t, g.dir)
		for _, reference := range bundleShaderReferences(t, files) {
			source, err := fs.ReadFile(files, reference.file)
			if err != nil {
				t.Errorf("%s: %s is not in the bundle", g.dir, reference.file)
				continue
			}
			// Core shaders are the same file in every game's bundle.
			key := sha256.Sum256([]byte(reference.file + "\x00" + reference.stage + "\x00" + string(source)))
			if !seen[key] {
				seen[key] = true
				jobs = append(jobs, job{bundle, reference})
			}
		}
	}
	if len(jobs) < 20 {
		t.Fatalf("found only %d shaders to compile", len(jobs))
	}

	var (
		wait     sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)
	queue := make(chan job)
	for range runtime.NumCPU() {
		wait.Go(func() {
			for next := range queue {
				file := filepath.Join(next.bundle.root, filepath.FromSlash(next.reference.file))
				if _, err := compileShader(file, next.reference.stage); err != nil {
					mu.Lock()
					failures = append(failures, err.Error())
					mu.Unlock()
				}
			}
		})
	}
	for _, next := range jobs {
		queue <- next
	}
	close(queue)
	wait.Wait()
	slices.Sort(failures)
	for _, failure := range failures {
		t.Error(failure)
	}
}

// loadProfiles runs the runtime's GPU profile loader on a profile file and
// returns the profile rows it wrote, PROFILE_ROWS (6) per id.
func loadProfiles(t *testing.T, bundle *shaderBundle, document []byte) []float32 {
	t.Helper()
	d := newWARPDevice(t)
	loader := d.shader("cs", bundle.compile(filepath.Join(bundle.root, "surface", "LoadProfilesJSON.hlsl"), "cs"))
	bytes := make([]uint32, len(document))
	for index, value := range document {
		bytes[index] = uint32(value)
	}
	const ids = 16
	profiles := d.structured(make([]float32, ids*6*4), 16)
	offsets := d.structured(make([]float32, ids*4), 16)
	d.dispatch(loader, bindings{
		srv: map[int]unsafe.Pointer{0: d.uints(bytes).srv},
		uav: map[int]unsafe.Pointer{0: profiles.uav, 1: offsets.uav},
	}, [3]int{1, 1, 1})
	return d.readFloats(profiles)
}

// The GPU loader must read every record length a profile file may hold with
// each number keeping its meaning, and reject a whole file that holds a
// record it does not know rather than load part of it.
func TestProfileLoaderReadsEveryRecordLength(t *testing.T) {
	bundle := newShaderBundle(t, games[0].dir)
	base := func(id int) []float64 {
		return []float64{float64(id), 1, .2, .3, .5, 0, 1, 0, 0, 0, 0, 1, 0, .01, .02, .03}
	}
	look := Look{
		Force: 2, Amount: .5, CustomColor: true, Tint: [3]float64{.2, .4, 1}, AbsorptionMM: 30, Viscosity: 4,
		Nozzle: NozzleOoze, Scattering: .35, Roughness: .2,
	}
	written := append(base(6), lookRecord(&look)...)
	legacy := [][]float64{
		base(0),
		append(base(1), 1, 1, .1, .2, .3, .04),
		append(base(2), 1, 1, .1, .2, .3, .04, .5),
		append(base(3), 1, 1, .1, .2, .3, .04, .5, 3),
		append(base(4), 1.5, 0, .1, .2, .3, .04, 0, 3, .7),
		append(base(5), 1.5, 0, .1, .2, .3, .04, 0, 3, .7, 1),
		written,
	}
	document := func(records [][]float64) []byte {
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		return []byte(`{"schema_version":1,"enabled":true,"gpu_data":` + string(data) + `,"mod_path":"x"}`)
	}

	rows := loadProfiles(t, bundle, document(legacy))
	row := func(id, k int) []float32 { return rows[(id*6+k)*4 : (id*6+k+1)*4] }
	for id, want := range map[int][3][]float32{
		0: {{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}},
		// Before runtime 24 the first look number and the width are dropped.
		1: {{0, 1, .04, 0}, {.1, .2, .3, 0}, {0, 0, 0, 0}},
		2: {{0, 1, .04, 0}, {.1, .2, .3, 0}, {0, 0, 0, 0}},
		3: {{0, 1, .04, 0}, {.1, .2, .3, 3}, {0, 0, 0, 0}},
		4: {{1.5, 0, .04, .7}, {.1, .2, .3, 3}, {0, 0, 0, 0}},
		5: {{1.5, 0, .04, .7}, {.1, .2, .3, 3}, {1, 0, 0, 0}},
		6: {{2, 1, .03, .5}, {.2, .4, 1, 4}, {2, .35, .2, 0}},
	} {
		if got := row(id, 0); !slices.Equal(got, []float32{1, .2, .3, .5}) {
			t.Errorf("record %d profile row = %v", id, got)
		}
		for k, values := range want {
			if got := row(id, 3+k); !closeFloats(got, values) {
				t.Errorf("record %d look row %d = %v, want %v", id, k, got, values)
			}
		}
	}

	for name, records := range map[string][][]float64{
		"partial material":   {base(0), append(base(1), 1, 0, .1, .2, .3, .04, 0, 1, 1, 0, .5)},
		"scattering above 1": {base(0), append(base(1), 1, 0, .1, .2, .3, .04, 0, 1, 1, 0, 1.5, 0)},
		"negative roughness": {base(0), append(base(1), 1, 0, .1, .2, .3, .04, 0, 1, 1, 0, 0, -.1)},
	} {
		for index, value := range loadProfiles(t, bundle, document(records)) {
			if value != 0 {
				t.Fatalf("%s: loader wrote %v at %d of a file it must reject", name, value, index)
			}
		}
	}
}

func closeFloats(got, want []float32) bool {
	for index := range want {
		if math.Abs(float64(got[index]-want[index])) > 1e-5 {
			return false
		}
	}
	return len(got) == len(want)
}
