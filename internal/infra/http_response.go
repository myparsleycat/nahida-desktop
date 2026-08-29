package infra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

const (
	cborContentType = "application/cbor"
	jsonContentType = "application/json"
)

var apiCBORDecMode = func() cbor.DecMode {
	mode, err := (cbor.DecOptions{
		DefaultMapType: reflect.TypeOf(map[string]any(nil)),
	}).DecMode()
	if err != nil {
		panic(err)
	}
	return mode
}()

// normalizeAPIResponse converts structured Nahida API responses into the JSON
// response contract consumed by the rest of the application. Keeping format
// negotiation here prevents individual API callers from needing CBOR branches.
func normalizeAPIResponse(response *http.Response) error {
	if response == nil || response.Body == nil || !isCBORContentType(response.Header.Get("Content-Type")) {
		return nil
	}
	raw, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return fmt.Errorf("read CBOR response: %w", err)
	}
	var value any
	if err := apiCBORDecMode.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode CBOR response: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode CBOR response as JSON: %w", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(encoded))
	response.ContentLength = int64(len(encoded))
	response.Header = response.Header.Clone()
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	response.Header.Set("Content-Type", jsonContentType)
	response.Header.Set("Content-Length", fmt.Sprintf("%d", len(encoded)))
	return nil
}

func isCBORContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		return strings.EqualFold(mediaType, cborContentType)
	}
	return strings.Contains(strings.ToLower(contentType), cborContentType)
}
