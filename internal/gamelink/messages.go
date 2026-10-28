package gamelink

import "fmt"

type MessageType uint16

const (
	TypeHello       MessageType = 0x0001
	TypeHelloReply  MessageType = 0x0002
	TypePing        MessageType = 0x0003
	TypePong        MessageType = 0x0004
	TypeError       MessageType = 0x00FF
	TypeGetVars     MessageType = 0x0010
	TypeVarsReply   MessageType = 0x0011
	TypeSetVars     MessageType = 0x0012
	TypeVarsApplied MessageType = 0x0013
	TypeEvent       MessageType = 0x0020
	TypePickArm     MessageType = 0x0100
	TypePickArmed   MessageType = 0x0101
	TypePickResult  MessageType = 0x0102
	TypePickFailed  MessageType = 0x0103
)

type RenderAPI uint8

const (
	RenderD3D11  RenderAPI = 1
	RenderD3D12  RenderAPI = 2
	RenderVulkan RenderAPI = 3
)

type Capability uint64

const (
	CapabilityVars      Capability = 1 << 0
	CapabilityPickD3D11 Capability = 1 << 1
)

const (
	ErrorUnsupportedVersion = "UNSUPPORTED_VERSION"
	ErrorBusy               = "BUSY"
)

type EventKind uint16

const (
	EventReloaded EventKind = 1
	EventShutdown EventKind = 2
)

type Message interface {
	Type() MessageType
	encode(e *encoder)
}

type Hello struct {
	Client     string
	MinVersion uint16
	MaxVersion uint16
}

type HelloReply struct {
	Version        uint16
	DLLVersion     string
	PID            uint32
	ExePath        string
	ImporterFolder string
	RenderAPI      RenderAPI
	Capabilities   Capability
}

type Ping struct{ Nonce uint64 }

type Pong struct{ Nonce uint64 }

// ErrorReply answers the request with the same ID; Code is one of the codes in
// PROTOCOL.md.
type ErrorReply struct {
	Code   string
	Detail string
}

type GetVars struct{ Names []string }

type Var struct {
	Name   string
	Exists bool
	Value  float32
}

type VarsReply struct{ Values []Var }

type Assign struct {
	Name  string
	Value float32
}

type SetVars struct{ Vars []Assign }

type VarsApplied struct {
	Frame  uint64
	Values []Var
}

type Event struct {
	Kind  EventKind
	Frame uint64
}

type CursorSource uint8

const (
	CursorCurrent CursorSource = 0
	CursorGiven   CursorSource = 1
)

type PickArm struct {
	Cursor CursorSource
	X, Y   float32
}

type PickArmed struct{ CaptureID uint64 }

type PickFailed struct {
	CaptureID uint64
	Code      string
	Detail    string
}

func (Hello) Type() MessageType       { return TypeHello }
func (HelloReply) Type() MessageType  { return TypeHelloReply }
func (Ping) Type() MessageType        { return TypePing }
func (Pong) Type() MessageType        { return TypePong }
func (ErrorReply) Type() MessageType  { return TypeError }
func (GetVars) Type() MessageType     { return TypeGetVars }
func (VarsReply) Type() MessageType   { return TypeVarsReply }
func (SetVars) Type() MessageType     { return TypeSetVars }
func (VarsApplied) Type() MessageType { return TypeVarsApplied }
func (Event) Type() MessageType       { return TypeEvent }
func (PickArm) Type() MessageType     { return TypePickArm }
func (PickArmed) Type() MessageType   { return TypePickArmed }
func (PickResult) Type() MessageType  { return TypePickResult }
func (PickFailed) Type() MessageType  { return TypePickFailed }

func (m Hello) encode(e *encoder) {
	e.str(m.Client)
	e.u16(m.MinVersion)
	e.u16(m.MaxVersion)
}

func (m HelloReply) encode(e *encoder) {
	e.u16(m.Version)
	e.str(m.DLLVersion)
	e.u32(m.PID)
	e.str(m.ExePath)
	e.str(m.ImporterFolder)
	e.u8(uint8(m.RenderAPI))
	e.u64(uint64(m.Capabilities))
}

func (m Ping) encode(e *encoder) { e.u64(m.Nonce) }
func (m Pong) encode(e *encoder) { e.u64(m.Nonce) }

func (m ErrorReply) encode(e *encoder) {
	e.str(m.Code)
	e.str(m.Detail)
}

func (m GetVars) encode(e *encoder) {
	e.count16(len(m.Names))
	for _, name := range m.Names {
		e.str(name)
	}
}

func (m VarsReply) encode(e *encoder) { encodeVars(e, m.Values) }

func (m SetVars) encode(e *encoder) {
	e.count16(len(m.Vars))
	for _, v := range m.Vars {
		e.str(v.Name)
		e.f32(v.Value)
	}
}

func (m VarsApplied) encode(e *encoder) {
	e.u64(m.Frame)
	encodeVars(e, m.Values)
}

func encodeVars(e *encoder, values []Var) {
	e.count16(len(values))
	for _, v := range values {
		e.str(v.Name)
		e.bool(v.Exists)
		e.f32(v.Value)
	}
}

func (m Event) encode(e *encoder) {
	e.u16(uint16(m.Kind))
	e.u64(m.Frame)
}

func (m PickArm) encode(e *encoder) {
	e.u8(uint8(m.Cursor))
	e.f32(m.X)
	e.f32(m.Y)
}

func (m PickArmed) encode(e *encoder) { e.u64(m.CaptureID) }

func (m PickFailed) encode(e *encoder) {
	e.u64(m.CaptureID)
	e.str(m.Code)
	e.str(m.Detail)
}

// Encode builds the frame of a message.
func Encode(requestID uint32, m Message) (Frame, error) {
	e := encoder{}
	m.encode(&e)
	if e.err != nil {
		return Frame{}, fmt.Errorf("encode %T: %w", m, e.err)
	}
	return Frame{Type: m.Type(), RequestID: requestID, Payload: e.buf}, nil
}

// Decode parses a frame's payload. A type it does not know returns
// ErrUnknownType, which receivers skip.
func Decode(frame Frame) (Message, error) {
	d := decoder{buf: frame.Payload}
	var m Message
	switch frame.Type {
	case TypeHello:
		m = Hello{Client: d.str(), MinVersion: d.u16(), MaxVersion: d.u16()}
	case TypeHelloReply:
		m = HelloReply{
			Version: d.u16(), DLLVersion: d.str(), PID: d.u32(), ExePath: d.str(), ImporterFolder: d.str(),
			RenderAPI: RenderAPI(d.u8()), Capabilities: Capability(d.u64()),
		}
	case TypePing:
		m = Ping{Nonce: d.u64()}
	case TypePong:
		m = Pong{Nonce: d.u64()}
	case TypeError:
		m = ErrorReply{Code: d.str(), Detail: d.str()}
	case TypeGetVars:
		names := make([]string, d.count16(2))
		for i := range names {
			names[i] = d.str()
		}
		m = GetVars{Names: names}
	case TypeVarsReply:
		m = VarsReply{Values: decodeVars(&d)}
	case TypeSetVars:
		vars := make([]Assign, d.count16(6))
		for i := range vars {
			vars[i] = Assign{Name: d.str(), Value: d.f32()}
		}
		m = SetVars{Vars: vars}
	case TypeVarsApplied:
		frameNo := d.u64()
		m = VarsApplied{Frame: frameNo, Values: decodeVars(&d)}
	case TypeEvent:
		m = Event{Kind: EventKind(d.u16()), Frame: d.u64()}
	case TypePickArm:
		m = PickArm{Cursor: CursorSource(d.u8()), X: d.f32(), Y: d.f32()}
	case TypePickArmed:
		m = PickArmed{CaptureID: d.u64()}
	case TypePickResult:
		m = decodePickResult(&d)
	case TypePickFailed:
		m = PickFailed{CaptureID: d.u64(), Code: d.str(), Detail: d.str()}
	default:
		return nil, fmt.Errorf("%w %#04x", ErrUnknownType, uint16(frame.Type))
	}
	if err := d.finish(); err != nil {
		return nil, fmt.Errorf("decode %#04x: %w", uint16(frame.Type), err)
	}
	return m, nil
}

func decodeVars(d *decoder) []Var {
	values := make([]Var, d.count16(7))
	for i := range values {
		values[i] = Var{Name: d.str(), Exists: d.bool(), Value: d.f32()}
	}
	return values
}
