package ui

import (
	"slices"
	"testing"
	"time"
)

// Copying a folder and a folder inside it would import the inner one twice, so a selection never holds both.
func TestToggleImportKeepsOneFolderPerSubtree(t *testing.T) {
	t.Parallel()

	im := &driveImport{selected: map[string]bool{}, nodes: map[string]*driveImportNode{
		"root":  {id: "root", isDir: true},
		"a":     {id: "a", parent: "root", isDir: true},
		"a1":    {id: "a1", parent: "a", isDir: true},
		"b":     {id: "b", parent: "root", isDir: true},
		"file":  {id: "file", parent: "b"},
		"cycle": {id: "cycle", parent: "cycle", isDir: true},
	}}
	selected := func() []string {
		var ids []string
		for id := range im.selected {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return ids
	}

	im.toggleImport("a1")
	im.toggleImport("b")
	im.toggleImport("a")
	if got := selected(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("after selecting a over a1 = %q, want [a b]", got)
	}
	im.toggleImport("a1")
	im.toggleImport("file")
	im.toggleImport("cycle")
	if got := selected(); !slices.Equal(got, []string{"a", "b", "cycle"}) {
		t.Fatalf("selecting under a selected folder or a file changed the selection to %q", got)
	}
	if partly := im.partlySelected(); !partly["root"] || partly["a"] {
		t.Fatalf("partly selected = %v, want root only", partly)
	}
}

// The drive service hands over the server's record as decoded JSON or CBOR maps; the top folder is the ancestor
// whose parentId is null.
func TestParseDriveListing(t *testing.T) {
	t.Parallel()

	updated := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	listing := parseDriveListing(map[string]any{
		"content": map[string]any{"id": "f1", "name": "Mods", "isDir": true},
		"children": []any{
			map[string]any{
				"id": "c1", "name": "a.zip", "isDir": false, "size": uint64(2048), "updatedAt": updated,
				"storing": true, "preview": map[string]any{"img": map[string]any{
					"default": "https://cdn/a.webp", "thumbnail": "https://cdn/a.thumb.webp", "cover": nil,
				}},
			},
			map[string]any{"id": "c2", "name": "b", "isDir": true, "size": nil, "updatedAt": "2026-03-01T09:30:00Z"},
			map[string]any{"name": "no id"},
		},
		"ancestors": []any{
			map[string]any{"id": "root", "name": "root", "parentId": nil, "depth": 0},
			map[string]any{"id": "f1", "name": "Mods", "parentId": "root", "depth": 1},
		},
		"parent": map[string]any{"id": "root", "parentId": nil},
	})

	if listing.content.Name != "Mods" || len(listing.children) != 2 {
		t.Fatalf("content = %q with %d children, want Mods with 2", listing.content.Name, len(listing.children))
	}
	file, dir := listing.children[0], listing.children[1]
	if file.Size != 2048 || !file.HasSize || !file.Storing || !file.Updated.Equal(updated) ||
		file.Thumb != "https://cdn/a.thumb.webp" || file.Image != "https://cdn/a.webp" {
		t.Fatalf("file = %+v", file)
	}
	if dir.HasSize || !dir.Updated.Equal(updated) {
		t.Fatalf("folder = %+v, want no size and the parsed date", dir)
	}
	if !listing.ancestors[0].Root || listing.ancestors[1].Root || listing.parent == nil || !listing.parent.Root {
		t.Fatalf("ancestors = %+v, parent = %+v", listing.ancestors, listing.parent)
	}
}
