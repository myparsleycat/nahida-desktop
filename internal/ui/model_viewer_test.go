package ui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/myparsleycat/fairy/icons/lucide"
)

func TestModelViewerOpeningKeepsTheLastSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a, b := filepath.Join(root, "A"), filepath.Join(root, "B")
	first := &ModelViewer{path: a, loading: true}
	if !first.blocksOpen(a, nil) || first.blocksOpen(b, nil) {
		t.Fatal("a second click on A should reuse its load, while B should replace it")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	first.openFailed = func(err error) { t.Errorf("the old viewer reported a late error: %v", err) }
	first.finishLoad(ctx, nil, errors.New("late failure"))
	first.finishLoad(ctx, &viewerScene{}, nil)
	if first.opened || first.scene != nil {
		t.Fatal("the cancelled viewer installed its late result")
	}

	second := &ModelViewer{path: b, opened: true}
	if !second.blocksOpen(a, nil) {
		t.Fatal("an open dialog allowed a background request to replace it")
	}
	first.blender = &viewerBlenderSource{link: 1}
	if !first.blocksOpen(b, &viewerBlenderSource{link: 1}) || first.blocksOpen(a, &viewerBlenderSource{link: 2}) ||
		first.blocksOpen(a, nil) {
		t.Fatal("Blender load identity should depend on the connection, independently of mod paths")
	}
}

func TestCaptureMenuOnlyOffersModPreviewForModFolders(t *testing.T) {
	t.Parallel()
	v := &ModelViewer{scene: &viewerScene{}}
	items := v.captureMenuItems()
	if len(items) != 2 || items[0].Icon != lucide.Camera || items[1].Icon != lucide.ImageDown {
		t.Fatal("a mod viewer should offer capture to the mod preview and PNG export")
	}
	v.blender = &viewerBlenderSource{link: 1}
	items = v.captureMenuItems()
	if len(items) != 1 || items[0].Icon != lucide.ImageDown || items[0].Submenu == nil || items[0].Disabled {
		t.Fatal("a Blender viewer should offer PNG export")
	}
}
