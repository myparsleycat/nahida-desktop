package drive

import (
	"context"
	"strings"

	"nahida.live/desktop/internal/infra"
)

func (d *Drive) listDestinationChildIDs(ctx context.Context, destinationID string) map[string]struct{} {
	ids := make(map[string]struct{})
	item, err := d.GetItem(ctx, destinationID)
	if err != nil {
		if d.log != nil {
			_ = infra.ReportError(d.log, err, "Drive:CopyFromUrl:ListDestinationChildrenFailed", infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "copy-from-url", Fields: map[string]any{"destinationId": destinationID}})
		}
		return ids
	}
	for _, child := range driveItemChildren(item) {
		if id, _ := child["id"].(string); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func (d *Drive) hasRemoteImportResult(ctx context.Context, destinationID string, expectedSize int64, preexistingChildIDs map[string]struct{}, sourceName string) bool {
	if ctx.Err() != nil {
		return false
	}
	destination, err := d.GetItem(ctx, destinationID)
	if err != nil {
		d.logImportVerificationFailure(destinationID, "", expectedSize, err)
		return false
	}
	newDirectories := make([]map[string]any, 0)
	for _, child := range driveItemChildren(destination) {
		id, _ := child["id"].(string)
		name, _ := child["name"].(string)
		isDir, _ := child["isDir"].(bool)
		if !isDir || id == "" || !isCollectionFolderName(name, sourceName) {
			continue
		}
		if _, existed := preexistingChildIDs[id]; !existed {
			newDirectories = append(newDirectories, child)
		}
	}
	for _, child := range newDirectories {
		if size, ok := remoteImportNumber(child, "size"); ok && size == expectedSize {
			return true
		}
	}
	pending := make([]string, 0, len(newDirectories))
	for _, child := range newDirectories {
		size, _ := remoteImportNumber(child, "size")
		if size > expectedSize {
			id, _ := child["id"].(string)
			pending = append(pending, id)
		}
	}
	visited := map[string]struct{}{destinationID: {}}
	for len(pending) > 0 && len(visited) < 128 {
		if ctx.Err() != nil {
			return false
		}
		last := len(pending) - 1
		itemID := pending[last]
		pending = pending[:last]
		if _, ok := visited[itemID]; ok || itemID == "" {
			continue
		}
		visited[itemID] = struct{}{}
		item, getErr := d.GetItem(ctx, itemID)
		if getErr != nil {
			d.logImportVerificationFailure(destinationID, itemID, expectedSize, getErr)
			return false
		}
		for _, child := range driveItemChildren(item) {
			isDir, _ := child["isDir"].(bool)
			if !isDir {
				continue
			}
			size, _ := remoteImportNumber(child, "size")
			if size == expectedSize {
				return true
			}
			if size > expectedSize {
				id, _ := child["id"].(string)
				pending = append(pending, id)
			}
		}
	}
	return false
}

func driveItemChildren(value any) []map[string]any {
	record, ok := asRecord(value)
	if !ok {
		return nil
	}
	raw, _ := record["children"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, child := range raw {
		if item, ok := asRecord(child); ok {
			out = append(out, item)
		}
	}
	return out
}

func (d *Drive) logImportVerificationFailure(destinationID, itemID string, expectedSize int64, err error) {
	if d.log == nil {
		return
	}
	d.log.Warn(map[string]any{
		"destinationId": destinationID, "itemId": itemID, "expectedSize": expectedSize,
		"error": err.Error(), "stage": "verify-server-copy",
	}, "Drive:CopyFromUrl:ServerImportVerificationFailed")
}

func (d *Drive) getOrCreateCollectionFolder(ctx context.Context, parentID, name string) (string, error) {
	if ctx.Err() != nil {
		return "", copyCanceledError()
	}
	sanitized := name
	if d.fs != nil {
		sanitized = d.fs.SanitizeWindowsFilename(name, " ")
	}
	current, err := d.GetItem(ctx, parentID)
	if err != nil {
		return "", err
	}
	if existing := findCollectionFolder(current, sanitized); existing != "" {
		return existing, nil
	}
	before := dirIDs(current)
	if _, err := d.CreateDir(ctx, parentID, sanitized); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", copyCanceledError()
	}
	updated, err := d.GetItem(ctx, parentID)
	if err != nil {
		return "", err
	}
	if created := findNewCollectionFolder(updated, before, sanitized); created != "" {
		return created, nil
	}
	if existing := findCollectionFolder(updated, sanitized); existing != "" {
		return existing, nil
	}
	return "", newDriveAPIError("DRIVE_COLLECTION_FOLDER_CREATE_FAILED", `The collection folder "`+sanitized+`" could not be created.`, 0, nil)
}

func findCollectionFolder(item any, name string) string {
	record, ok := asRecord(item)
	if !ok {
		return ""
	}
	children, _ := record["children"].([]any)
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			continue
		}
		isDir, _ := rec["isDir"].(bool)
		childName, _ := rec["name"].(string)
		id, _ := rec["id"].(string)
		if isDir && childName == name && id != "" {
			return id
		}
	}
	return ""
}

func dirIDs(item any) map[string]struct{} {
	out := map[string]struct{}{}
	record, ok := asRecord(item)
	if !ok {
		return out
	}
	children, _ := record["children"].([]any)
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			continue
		}
		isDir, _ := rec["isDir"].(bool)
		id, _ := rec["id"].(string)
		if isDir && id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func findNewCollectionFolder(item any, before map[string]struct{}, name string) string {
	record, ok := asRecord(item)
	if !ok {
		return ""
	}
	children, _ := record["children"].([]any)
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			continue
		}
		isDir, _ := rec["isDir"].(bool)
		id, _ := rec["id"].(string)
		childName, _ := rec["name"].(string)
		if !isDir || id == "" {
			continue
		}
		if _, existed := before[id]; existed {
			continue
		}
		if isCollectionFolderName(childName, name) {
			return id
		}
	}
	return ""
}

func isCollectionFolderName(actual, expected string) bool {
	if actual == expected {
		return true
	}
	prefix := expected + " ("
	if !strings.HasPrefix(actual, prefix) || !strings.HasSuffix(actual, ")") {
		return false
	}
	inner := actual[len(prefix) : len(actual)-1]
	if inner == "" {
		return false
	}
	for _, r := range inner {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
