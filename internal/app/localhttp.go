package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"

	"nahida.live/desktop/internal/drive"
)

type localHTTPMessage struct {
	Type          string                  `cbor:"type"`
	ID            *string                 `cbor:"id"`
	IsDir         *bool                   `cbor:"isDir"`
	Link          *drive.DownloadLink     `cbor:"link"`
	SuggestedName *string                 `cbor:"suggestedName"`
	Data          *drive.DownloadMetadata `cbor:"data"`
	Title         *string                 `cbor:"title"`
	FileURL       *string                 `cbor:"fileUrl"`
}

func (rt *runtime) handleLocalHTTPMessage(ctx context.Context, payload []byte) (string, error) {
	var message localHTTPMessage
	if err := cbor.Unmarshal(payload, &message); err != nil {
		return "invalid data", fmt.Errorf("decode extension message: %w", err)
	}
	switch message.Type {
	case "live":
		return rt.handleLiveDownload(ctx, message)
	case "hui":
		if message.Title == nil || message.FileURL == nil {
			return "download error", errors.New("invalid hui download request")
		}
		if rt.mod == nil {
			return "download error", errors.New("hui download services are not configured")
		}
		status, err := rt.mod.HuiDownload(ctx, *message.Title, *message.FileURL)
		if err != nil {
			return "download error", err
		}
		if status == "canceled" {
			return "download canceled", nil
		}
		return "download " + status, nil
	default:
		// Preserve the Electron bridge's response for an unknown message type.
		return "download started", nil
	}
}

func (rt *runtime) handleLiveDownload(ctx context.Context, message localHTTPMessage) (string, error) {
	if message.ID == nil {
		return "download error", errors.New("live download id is required")
	}
	if message.Link == nil {
		if rt.auth == nil {
			return "download error", errors.New("auth service is not configured")
		}
		loggedIn, err := rt.auth.IsLoggedIn(ctx)
		if err != nil {
			return "download error", err
		}
		if !loggedIn {
			return "unauthorized", nil
		}
	}
	isDir := true
	if message.IsDir != nil {
		isDir = *message.IsDir
	}
	name := "item"
	if message.SuggestedName != nil {
		name = *message.SuggestedName
	} else if message.Data != nil {
		name = message.Data.Root.Name
	}
	if rt.drive == nil {
		return "download error", errors.New("drive service is not configured")
	}
	result, err := rt.drive.StartDownload(ctx, drive.StartDownloadParams{
		Items: []drive.DownloadItem{{ID: *message.ID, IsDir: isDir, Name: name}},
		Link:  message.Link,
		Data:  message.Data,
	})
	if err != nil {
		return "download error", err
	}
	return "download " + result.Status, nil
}
