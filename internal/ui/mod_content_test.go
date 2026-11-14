package ui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/mod"
)

// Toggling renames the mod folder, and a new key would decode the same preview again and flash the card.
func TestModPreviewKeyKeepsToggledMods(t *testing.T) {
	key := func(folder, image, version string) string {
		return modPreviewKey(mod.ModInfo{Path: folder, PreviewVersion: version, Mtime: 1}, folder+`\`+image)
	}
	base := key(`D:\Mods\Group\Character`, "preview.png", "1:2")
	for _, folder := range []string{
		`D:\Mods\Group\DISABLED Character`,
		`D:\Mods\Group\DISABLED_Character`,
		`D:\Mods\DISABLED Group\DISABLED Character`,
	} {
		if got := key(folder, "preview.png", "1:2"); got != base {
			t.Errorf("key(%s) = %s, want %s", folder, got, base)
		}
	}

	// Mods keep variants in DISABLED folders of their own, and each holds a different image.
	if key(`D:\Mods\Character`, `Variant\preview.png`, "1:2") ==
		key(`D:\Mods\Character`, `DISABLED Variant\preview.png`, "1:2") {
		t.Error("the variants of one mod share a key")
	}
	// The light listing has no preview version, so only the path tells a mod apart from its disabled sibling.
	if key(`D:\Mods\Character`, "preview.png", "") == key(`D:\Mods\DISABLED Character`, "preview.png", "") {
		t.Error("siblings without a preview version share a key")
	}
}

func TestSizedModPreviewRetriesRenamedFileWithThumbnail(t *testing.T) {
	t.Parallel()
	for _, pending := range []bool{false, true} {
		name := "failed read"
		if pending {
			name = "pending read"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			oldFolder := filepath.Join(root, "Character")
			newFolder := filepath.Join(root, "DISABLED Character")
			if err := os.Mkdir(oldFolder, 0o700); err != nil {
				t.Fatal(err)
			}
			var data bytes.Buffer
			if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 512, 512))); err != nil {
				t.Fatal(err)
			}
			oldPath := filepath.Join(oldFolder, "preview.png")
			newPath := filepath.Join(newFolder, "preview.png")
			if err := os.WriteFile(oldPath, data.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			key := "mod-preview-test:" + root
			t.Cleanup(func() { fairy.Images.Forget(key) })
			wait := func(what string, ready func() bool) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for !ready() {
					if time.Now().After(deadline) {
						t.Fatalf("timed out waiting for %s", what)
					}
					time.Sleep(time.Millisecond)
				}
			}
			s := &Shell{}
			var thumbnail *fairy.Image
			wait("thumbnail", func() bool {
				thumbnail = s.sizedModPreview(key, oldPath, 40, 40, fairy.FitContain)
				return thumbnail != nil
			})

			started, proceed := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-proceed:
				default:
					close(proceed)
				}
			})
			fairy.Images.Sized(key, 512, 512, fairy.FitContain, func() ([]byte, error) {
				close(started)
				<-proceed
				return os.ReadFile(oldPath)
			})
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("large image read did not start")
			}
			if err := os.Rename(oldFolder, newFolder); err != nil {
				t.Fatal(err)
			}
			if pending {
				if got := s.sizedModPreview(key, newPath, 512, 512, fairy.FitContain); got != thumbnail {
					t.Fatal("pending read discarded the cached thumbnail")
				}
			}
			close(proceed)
			wait("old-path failure", func() bool {
				return fairy.Images.SizedErr(key, 512, 512, fairy.FitContain) != nil
			})

			wait("full-size image from renamed file", func() bool {
				img := s.sizedModPreview(key, newPath, 512, 512, fairy.FitContain)
				if img == nil {
					return false
				}
				width, height := img.Size()
				return width == 512 && height == 512
			})
			if err := fairy.Images.SizedErr(key, 512, 512, fairy.FitContain); err != nil {
				t.Fatalf("renamed preview still has a cached error: %v", err)
			}
		})
	}
}
