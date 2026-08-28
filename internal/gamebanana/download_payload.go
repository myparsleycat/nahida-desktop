package gamebanana

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var gameBananaIDToImporter = map[int]string{
	8552:  "GIMI",
	18366: "SRMI",
	10349: "HIMI",
	19567: "ZZMI",
	20357: "WWMI",
	21842: "EFMI",
	23012: "NTE",
}

type DownloadFileInput struct {
	ItemID    int    `json:"itemId"`
	FileID    int    `json:"fileId"`
	ModelName string `json:"modelName,omitempty"`
}

type DownloadFilePayload struct {
	FileURL      string  `json:"fileUrl"`
	Title        string  `json:"title"`
	CategoryName string  `json:"categoryName"`
	ImporterKey  *string `json:"importerKey"`
	PreviewURL   *string `json:"previewUrl"`
	ModID        int     `json:"modId"`
	ModPageURL   string  `json:"modPageUrl"`
	AuthorName   *string `json:"authorName"`
	AuthorURL    *string `json:"authorUrl"`
	FileMD5      *string `json:"fileMd5"`
	Version      *string `json:"version"`
}

func (g *GameBanana) GetDownloadFilePayload(ctx context.Context, input DownloadFileInput) (DownloadFilePayload, error) {
	model, err := normalizeModelName(input.ModelName)
	if err != nil {
		return DownloadFilePayload{}, err
	}
	profile, err := g.getModProfile(ctx, input.ItemID, model)
	if err != nil {
		return DownloadFilePayload{}, err
	}
	files, _ := profile["_aFiles"].([]any)
	var file map[string]any
	for _, entry := range files {
		record, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if jsonInt(record["_idRow"]) == input.FileID {
			file = record
			break
		}
	}
	if file == nil {
		return DownloadFilePayload{}, errors.New("GAMEBANANA_FILE_NOT_FOUND")
	}
	category, _ := profile["_aCategory"].(map[string]any)
	game, _ := profile["_aGame"].(map[string]any)
	submitter, _ := profile["_aSubmitter"].(map[string]any)
	importer := importerForGameBananaID(jsonInt(game["_idRow"]))
	preview := modPreviewURL(profile)
	payload := DownloadFilePayload{
		FileURL:      jsonString(file["_sDownloadUrl"]),
		Title:        jsonString(file["_sFile"]),
		CategoryName: jsonString(category["_sName"]),
		ImporterKey:  importer,
		PreviewURL:   preview,
		ModID:        jsonInt(profile["_idRow"]),
		ModPageURL:   jsonString(profile["_sProfileUrl"]),
		AuthorName:   jsonStringPtr(submitter["_sName"]),
		AuthorURL:    jsonStringPtr(submitter["_sProfileUrl"]),
		FileMD5:      jsonStringPtr(file["_sMd5Checksum"]),
		Version:      jsonStringPtr(file["_sVersion"]),
	}
	return payload, nil
}

func (g *GameBanana) getModProfile(ctx context.Context, itemID int, model string) (map[string]any, error) {
	g.mu.Lock()
	if g.lastProfileID == itemID && g.lastModelName == model && g.lastModProfile != nil {
		cloned := cloneRecord(g.lastModProfile)
		g.mu.Unlock()
		return cloned, nil
	}
	g.mu.Unlock()
	value, err := g.getJSON(ctx, http.MethodGet, fmt.Sprintf("/%s/%d/ProfilePage", model, itemID), nil, submissionReferer(model, itemID), modelResponseSchema(model, "profile", modProfileSchema))
	if err != nil {
		return nil, err
	}
	profile, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("GAMEBANANA_SCHEMA_ERROR:mod_profile")
	}
	g.mu.Lock()
	g.lastProfileID = itemID
	g.lastModelName = model
	g.lastModProfile = cloneRecord(profile)
	g.mu.Unlock()
	return profile, nil
}

func importerForGameBananaID(id int) *string {
	if name, ok := gameBananaIDToImporter[id]; ok {
		return &name
	}
	return nil
}

func modPreviewURL(profile map[string]any) *string {
	previewContent, _ := profile["_aPreviewContent"].(map[string]any)
	screenshots, _ := previewContent["screenshots"].([]any)
	if len(screenshots) == 0 {
		return nil
	}
	preview, _ := screenshots[0].(map[string]any)
	if preview == nil {
		return nil
	}
	candidates := []any{preview["_sFile"], preview["_sFile530"], preview["_sFile220"], preview["_sUrl"]}
	for _, candidate := range candidates {
		value := jsonString(candidate)
		lower := strings.ToLower(value)
		if value != "" && (strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
			return &value
		}
	}
	var relative string
	for _, candidate := range candidates {
		if value := jsonString(candidate); value != "" {
			relative = value
			break
		}
	}
	base := jsonString(preview["_sBaseUrl"])
	if relative == "" || base == "" {
		if strings.HasPrefix(strings.ToLower(base), "http://") || strings.HasPrefix(strings.ToLower(base), "https://") {
			return &base
		}
		return nil
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	resolved, err := url.Parse(base)
	if err != nil {
		return nil
	}
	ref, err := url.Parse(relative)
	if err != nil {
		return nil
	}
	joined := resolved.ResolveReference(ref).String()
	return &joined
}

func jsonInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func jsonString(value any) string {
	typed, _ := value.(string)
	return typed
}

func jsonStringPtr(value any) *string {
	typed, ok := value.(string)
	if !ok {
		return nil
	}
	return &typed
}
