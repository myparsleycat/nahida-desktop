package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	agentactions "nahida.live/desktop/internal/agent/actions"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	maxMessageImages = 4
	maxImageBytes    = 5 << 20
	imageDirectory   = "agent/images"
)

// supportedImageMIME maps accepted image types to the extension used for stored files.
var supportedImageMIME = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// pendingImage is one decoded image that is about to be stored below the app data directory.
type pendingImage struct {
	Name     string
	MIMEType string
	Data     []byte
}

// decodeImageInputs validates renderer attachments and decodes their base64 payloads.
func decodeImageInputs(inputs []AgentImageInput) ([]pendingImage, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if len(inputs) > maxMessageImages {
		return nil, fmt.Errorf("at most %d images can be attached to one message", maxMessageImages)
	}
	images := make([]pendingImage, 0, len(inputs))
	for _, input := range inputs {
		mimeType := strings.ToLower(strings.TrimSpace(input.MIMEType))
		if _, supported := supportedImageMIME[mimeType]; !supported {
			return nil, fmt.Errorf("unsupported image type %q", input.MIMEType)
		}
		// Base64 expands by 4/3, so the encoded length rules out oversized payloads before decoding.
		if len(input.Data) > maxImageBytes/3*4+4 {
			return nil, oversizedImageError(input.Name)
		}
		data, err := base64.StdEncoding.DecodeString(input.Data)
		if err != nil {
			return nil, fmt.Errorf("decode image %q: %w", imageLabel(input.Name), err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("image %q is empty", imageLabel(input.Name))
		}
		if len(data) > maxImageBytes {
			return nil, oversizedImageError(input.Name)
		}
		images = append(images, pendingImage{Name: strings.TrimSpace(input.Name), MIMEType: mimeType, Data: data})
	}
	return images, nil
}

func oversizedImageError(name string) error {
	return fmt.Errorf("image %q exceeds the %d MB limit", imageLabel(name), maxImageBytes>>20)
}

func imageLabel(name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return "attachment"
}

// storeImages writes images below the app data directory and returns their references. Images live
// as files so a conversation can be replayed on later turns without base64 payloads in the
// database.
func (s *Service) storeImages(sessionID string, images []pendingImage) ([]AgentImage, error) {
	if len(images) == 0 {
		return nil, nil
	}
	if s.appData == nil {
		return nil, errors.New("app data store is unavailable")
	}
	stored := make([]AgentImage, 0, len(images))
	stagedPaths := make([]string, 0, len(images))
	for _, image := range images {
		relative := filepath.Join(imageDirectory, sessionID, uuid.NewString()+supportedImageMIME[image.MIMEType])
		stagedPaths = append(stagedPaths, relative)
		if err := s.appData.WriteFile(relative, image.Data, 0o600); err != nil {
			s.removeImageFiles(stagedPaths)
			return nil, err
		}
		source, err := s.appData.Resolve(relative)
		if err != nil {
			s.removeImageFiles(stagedPaths)
			return nil, err
		}
		stored = append(stored, AgentImage{
			Name: image.Name, MIMEType: image.MIMEType, Bytes: len(image.Data), Path: relative, Src: source,
		})
	}
	return stored, nil
}

func (s *Service) cleanupStoredImages(images []AgentImage) {
	if len(images) == 0 {
		return
	}
	paths := make([]string, 0, len(images))
	for _, image := range images {
		paths = append(paths, image.Path)
	}
	s.removeImageFiles(paths)
}

func (s *Service) removeImageFiles(relativePaths []string) {
	if s.appData == nil {
		return
	}
	for _, relative := range relativePaths {
		path, err := s.appData.Resolve(relative)
		if err != nil {
			_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "agent-image", Stage: "cleanup",
				Fields: map[string]any{"path": relative},
			})
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "agent-image", Stage: "cleanup",
				Fields: map[string]any{"path": relative},
			})
		}
	}
}

// loadMessageImages reads stored images back for a provider request. An unreadable file is
// reported and skipped so one missing image cannot fail the whole turn.
func (s *Service) loadMessageImages(images []AgentImage) []MessageImage {
	if s == nil || s.appData == nil || len(images) == 0 {
		return nil
	}
	parts := make([]MessageImage, 0, len(images))
	for _, image := range images {
		data, err := s.appData.ReadFile(image.Path)
		if err != nil {
			_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "agent-image", Stage: "load",
				Fields: map[string]any{"path": image.Path, "mimeType": image.MIMEType},
			})
			continue
		}
		parts = append(parts, MessageImage{
			MIMEType: image.MIMEType,
			Data:     base64.StdEncoding.EncodeToString(data),
		})
	}
	return parts
}

// splitImageContent replaces image items of an MCP tool result with a text placeholder and returns
// the extracted images. The model then receives the picture itself instead of base64 text, and the
// persisted tool result stays small enough to keep in context.
func splitImageContent(result *mcp.CallToolResult) (*mcp.CallToolResult, []pendingImage) {
	if result == nil {
		return nil, nil
	}
	images := make([]pendingImage, 0, len(result.Content))
	content := make([]mcp.Content, 0, len(result.Content))
	for _, item := range result.Content {
		image, isImage := item.(*mcp.ImageContent)
		if !isImage {
			content = append(content, item)
			continue
		}
		mimeType := strings.ToLower(strings.TrimSpace(image.MIMEType))
		data, err := base64.StdEncoding.DecodeString(string(image.Data))
		if _, supported := supportedImageMIME[mimeType]; !supported || err != nil || len(data) == 0 {
			// Keep the original item when it cannot be stored, rather than silently dropping it.
			content = append(content, item)
			continue
		}
		images = append(images, pendingImage{MIMEType: mimeType, Data: data})
		content = append(content, &mcp.TextContent{
			Text: fmt.Sprintf("[image: %s, %d bytes]", mimeType, len(data)),
		})
	}
	if len(images) == 0 {
		return result, nil
	}
	trimmed := *result
	trimmed.Content = content
	return &trimmed, images
}

// splitActionImage turns an action result that carries an image into the metadata the run persists
// and the image the model receives, mirroring splitImageContent for MCP results.
func splitActionImage(output any) (any, []pendingImage) {
	captured, isImage := output.(agentactions.CapturedImage)
	if !isImage {
		return output, nil
	}

	persisted := map[string]any{
		"window": captured.Window,
		"width":  captured.Width,
		"height": captured.Height,
		"scale":  captured.Scale,
		"image":  fmt.Sprintf("[image: %s, %d bytes]", captured.MIMEType, len(captured.PNG)),
	}
	image := pendingImage{
		Name:     captureImageName(captured.Window),
		MIMEType: captured.MIMEType,
		Data:     captured.PNG,
	}
	return persisted, []pendingImage{image}
}

// captureImageName labels a capture in the chat, where the stored file name is a generated id.
func captureImageName(window platform.WindowInfo) string {
	if title := strings.TrimSpace(window.Title); title != "" {
		return title
	}
	if process := strings.TrimSpace(window.ProcessName); process != "" {
		return process
	}
	return "window capture"
}
