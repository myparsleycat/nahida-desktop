package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
)

// The Blender MCP add-on ("lab_blender_org.mcp", shipped with Blender 5.1+) exposes a raw TCP
// socket bridge rather than an HTTP MCP endpoint. One request is one connection: the client sends
// a NUL-terminated JSON command, the add-on executes the embedded Python on Blender's main thread
// and answers with a NUL-terminated JSON response before closing the socket.
//
// This file is the compatibility layer that lets Nahida Agent reach that add-on directly, so the
// user does not have to install the separate `blender-mcp` Python bridge.

const (
	blenderDefaultHost = "localhost"
	blenderDefaultPort = 9876
	// Renders and viewport captures block Blender's main thread for a long time; the reference
	// bridge also waits five minutes before giving up.
	blenderRequestTimeout = 300 * time.Second
	blenderDialTimeout    = 15 * time.Second
	blenderMaxResponse    = 64 << 20
	blenderReadBuffer     = 64 << 10
)

// mcpTransportBlender is the transport name of the built-in Blender compatibility layer. Selecting
// it makes Nahida host the MCP server in-process and translate tool calls into Blender-side Python
// sent over the add-on's socket, instead of launching an external MCP server.
const mcpTransportBlender = db.BlenderMCPTransport

// blenderEndpoint identifies one Blender add-on socket bridge.
type blenderEndpoint struct {
	Host string
	Port int
}

// blenderResponse is the add-on's reply to a single `execute` request.
type blenderResponse struct {
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result"`
	Message string          `json:"message"`
	Stdout  string          `json:"stdout"`
	Stderr  string          `json:"stderr"`
}

// blenderExecuteRequest is the only request type the add-on accepts.
type blenderExecuteRequest struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	StrictJSON bool   `json:"strict_json"`
}

func (e blenderEndpoint) address() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// blenderEndpointOverride, when set, pins every built-in Blender tool call to one instance. Tests
// use it to point the transport at a fake add-on without touching process-wide environment.
var blenderEndpointOverride *blenderEndpoint

// blenderEndpointFromEnv reads the same BLENDER_MCP_HOST/BLENDER_MCP_PORT overrides the reference
// bridge honors, so an environment variable entry on the MCP server row can retarget the socket.
func blenderEndpointFromEnv() blenderEndpoint {
	if blenderEndpointOverride != nil {
		return *blenderEndpointOverride
	}
	endpoint := blenderEndpoint{Host: blenderDefaultHost, Port: blenderDefaultPort}
	if host := strings.TrimSpace(os.Getenv("BLENDER_MCP_HOST")); host != "" {
		endpoint.Host = host
	}
	if raw := strings.TrimSpace(os.Getenv("BLENDER_MCP_PORT")); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 && port <= 65535 {
			endpoint.Port = port
		}
	}
	return endpoint
}

// executeBlenderCode runs code inside the connected Blender instance and returns the add-on's
// reply. A transport-level failure is returned as an error; a Blender-side exception is returned
// in the response with Status "error" so callers can surface it as a tool result.
func executeBlenderCode(endpoint blenderEndpoint, code string, strictJSON bool) (*blenderResponse, error) {
	return executeBlenderCodeWithin(endpoint, code, strictJSON, blenderRequestTimeout)
}

func executeBlenderCodeWithin(
	endpoint blenderEndpoint,
	code string,
	strictJSON bool,
	timeout time.Duration,
) (*blenderResponse, error) {
	payload, err := json.Marshal(blenderExecuteRequest{Type: "execute", Code: code, StrictJSON: strictJSON})
	if err != nil {
		return nil, fmt.Errorf("encode Blender request: %w", err)
	}

	conn, err := net.DialTimeout("tcp", endpoint.address(), blenderDialTimeout)
	if err != nil {
		return nil, fmt.Errorf(
			"cannot connect to Blender at %s: ensure Blender is running with the MCP add-on enabled "+
				"and its bridge server started: %w",
			endpoint.address(),
			err,
		)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set Blender socket deadline: %w", err)
	}

	request := make([]byte, 0, len(payload)+1)
	request = append(request, payload...)
	request = append(request, 0)
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("send request to Blender at %s: %w", endpoint.address(), err)
	}

	response, err := readBlenderResponse(conn, endpoint, timeout)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// readBlenderResponse accumulates bytes until the NUL delimiter that terminates the reply.
func readBlenderResponse(conn net.Conn, endpoint blenderEndpoint, timeout time.Duration) (*blenderResponse, error) {
	buffer := make([]byte, 0, blenderReadBuffer)
	chunk := make([]byte, blenderReadBuffer)

	for {
		read, err := conn.Read(chunk)
		if read > 0 {
			buffer = append(buffer, chunk[:read]...)
			if len(buffer) > blenderMaxResponse {
				return nil, fmt.Errorf("response from Blender exceeded %d bytes", blenderMaxResponse)
			}
			if index := indexNUL(buffer); index >= 0 {
				return decodeBlenderResponse(buffer[:index], endpoint)
			}
		}
		if err != nil {
			return nil, classifyBlenderReadError(err, endpoint, len(buffer), timeout)
		}
	}
}

func decodeBlenderResponse(payload []byte, endpoint blenderEndpoint) (*blenderResponse, error) {
	var response blenderResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("invalid response from Blender at %s: %w", endpoint.address(), err)
	}
	if response.Status == "" {
		return nil, fmt.Errorf("invalid response from Blender at %s: missing status", endpoint.address())
	}
	return &response, nil
}

func classifyBlenderReadError(err error, endpoint blenderEndpoint, received int, timeout time.Duration) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("no answer from Blender at %s within %s", endpoint.address(), timeout)
	}
	if received == 0 {
		return fmt.Errorf("no response from Blender at %s: %w", endpoint.address(), err)
	}
	return fmt.Errorf("read response from Blender at %s: %w", endpoint.address(), err)
}

func indexNUL(buffer []byte) int {
	for index, value := range buffer {
		if value == 0 {
			return index
		}
	}
	return -1
}

// blenderResultText renders the add-on's `result` payload for an MCP text response. Blender code
// that produced no JSON-serializable value answers with `{}`; that is reported as such instead of
// an empty string so the model can tell "no value" from "empty result".
func blenderResultText(response *blenderResponse) string {
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return "Blender returned no result."
	}
	var decoded any
	if err := json.Unmarshal(response.Result, &decoded); err == nil {
		if encoded, err := json.MarshalIndent(decoded, "", "  "); err == nil {
			return string(encoded)
		}
	}
	return string(response.Result)
}

// blenderErrorText builds the message for a failed execution, preferring the add-on's Python
// traceback and appending any captured stderr that the traceback does not already contain.
func blenderErrorText(response *blenderResponse) string {
	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = "Blender reported an unspecified error."
	}
	if stderr := strings.TrimSpace(response.Stderr); stderr != "" && !strings.Contains(message, stderr) {
		message += "\n\nstderr:\n" + stderr
	}
	return message
}
