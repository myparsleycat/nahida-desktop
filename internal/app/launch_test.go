package app

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/myparsleycat/fairy"
)

func TestModelViewerArgument(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"absolute", []string{"--model-viewer", `C:\Mods\모드 (1)`}, `C:\Mods\모드 (1)`},
		{"relative", []string{"--model-viewer", `..\모드 (1)`}, `D:\모드 (1)`},
		{"slashes", []string{"--model-viewer", `C:/Mods/A/../B`}, `C:\Mods\B`},
		{"missing", []string{"--model-viewer"}, ""},
		{"empty", []string{"--model-viewer", " "}, ""},
		{"other flag", []string{"--model-viewer", "--hidden"}, ""},
		{"duplicate", []string{"--model-viewer", `C:\First`, "--model-viewer", `C:\Second`}, `C:\First`},
		{"skip invalid", []string{"--model-viewer", "--model-viewer", `C:\Second`}, `C:\Second`},
		{"deep link", []string{"nahida://gamebanana/42", "--model-viewer", `C:\Mods`}, `C:\Mods`},
		{"deep link is not folder", []string{"--model-viewer", "nahida://gamebanana/42"}, ""},
		{"exact flag", []string{`--model-viewer=C:\Mods`}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelViewerArgument(append([]string{"app.exe"}, tt.args...), `D:\Working`); got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLaunchHandlerOpensModelViewerWithoutMain(t *testing.T) {
	var got []string
	handler := newLaunchHandler(
		func(path string) { got = append(got, path) },
		func() { t.Fatal("unexpected main window") },
		func([]string) { got = append(got, "arguments") },
		func([]string) { t.Fatal("unexpected download") },
		func(string, bool) {},
	)
	handler(fairy.Launch{Args: []string{"app", "--model-viewer", "first", "nahida://gamebanana/42"}, Dir: `C:\Mods`})
	handler(fairy.Launch{Args: []string{"app", "--model-viewer", `D:\Other`}})
	if want := []string{`C:\Mods\first`, `D:\Other`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("launches = %v, want %v", got, want)
	}
}

func TestLaunchDispatcherPreservesColdAndForwardedLaunches(t *testing.T) {
	d := &launchDispatcher{}
	var got []string
	d.Enqueue(fairy.Launch{Args: []string{"app", "queued"}})
	handler := newLaunchHandler(
		func(string) { t.Fatal("unexpected model viewer") },
		func() { got = append(got, "main") },
		func(args []string) { got = append(got, "arguments "+args[1]) },
		func([]string) {},
		func(string, bool) {},
	)
	d.Start(fairy.Launch{Args: []string{"app", "first"}}, handler)
	d.Enqueue(fairy.Launch{Args: []string{"app", "later"}})
	if want := []string{"main", "arguments queued", "arguments later"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("launches = %v, want %v", got, want)
	}
}

func TestLaunchDispatcherAllowsReentrantLaunch(t *testing.T) {
	d := &launchDispatcher{}
	var got []string
	d.Start(fairy.Launch{Args: []string{"first"}}, func(launch fairy.Launch) {
		got = append(got, launch.Args[0])
		if launch.Args[0] == "first" {
			d.Enqueue(fairy.Launch{Args: []string{"second"}})
		}
	})
	if !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatal(got)
	}
}

func TestLaunchHandlerDispatchesDownloadOnColdAndForwardedLaunches(t *testing.T) {
	var got []string
	handler := newLaunchHandler(
		func(string) { t.Fatal("unexpected model viewer") },
		func() { got = append(got, "main") },
		func([]string) { got = append(got, "arguments") },
		func(args []string) { got = append(got, "download "+args[1]) },
		func(string, bool) {},
	)
	handler(fairy.Launch{Args: []string{"app", "cold"}})
	handler(fairy.Launch{Args: []string{"app", "forwarded"}})
	if want := []string{"main", "download cold", "arguments", "download forwarded"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("launches = %v, want %v", got, want)
	}
}

func TestLaunchHandlerDispatchesQuickStartWithoutOpeningMain(t *testing.T) {
	var got []string
	handler := newLaunchHandler(
		func(string) { t.Fatal("unexpected model viewer") },
		func() { t.Fatal("unexpected main window") },
		func([]string) { t.Fatal("unexpected argument handler") },
		func([]string) { t.Fatal("unexpected download") },
		func(key string, first bool) { got = append(got, fmt.Sprintf("%s:%t", key, first)) },
	)
	handler(fairy.Launch{Args: []string{"app", "--xxmi-launch", "GIMI"}})
	handler(fairy.Launch{Args: []string{"app", "--xxmi-launch", "WWMI"}})
	if want := []string{"GIMI:true", "WWMI:false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("quick starts = %v, want %v", got, want)
	}
	if key := xxmiLaunchArgument([]string{"app", "--xxmi-launch", "unknown"}); key != "" {
		t.Fatalf("accepted unknown importer %q", key)
	}
}

func TestLaunchDispatcherQueuesQuickStartDuringStartup(t *testing.T) {
	d := &launchDispatcher{}
	d.Enqueue(fairy.Launch{Args: []string{"app", "--xxmi-launch", "WWMI"}})
	var got []string
	d.Start(fairy.Launch{Args: []string{"app", "--xxmi-launch", "GIMI"}}, newLaunchHandler(
		func(string) { t.Fatal("unexpected model viewer") },
		func() { t.Fatal("unexpected main window") },
		func([]string) { t.Fatal("unexpected argument handler") },
		func([]string) { t.Fatal("unexpected download") },
		func(key string, first bool) { got = append(got, fmt.Sprintf("%s:%t", key, first)) },
	))
	if want := []string{"GIMI:true", "WWMI:false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queued quick starts = %v, want %v", got, want)
	}
}
