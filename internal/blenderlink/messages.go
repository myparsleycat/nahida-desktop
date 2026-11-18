package blenderlink

import (
	"encoding/hex"
	"fmt"
)

type MessageType uint16

const (
	TypeHello         MessageType = 0x01
	TypeScene         MessageType = 0x02
	TypeMeshBegin     MessageType = 0x10
	TypeMeshChunk     MessageType = 0x11
	TypeMeshCommit    MessageType = 0x12
	TypeTextureBegin  MessageType = 0x13
	TypeTextureChunk  MessageType = 0x14
	TypeTextureCommit MessageType = 0x15
	TypePing          MessageType = 0x20
	TypeClientError   MessageType = 0x30
	TypeHelloAck      MessageType = 0x81
	TypeAck           MessageType = 0x82
	TypePong          MessageType = 0xA0
)

type PixelFormat uint8

// PixelRGBA8 is four bytes per pixel in R, G, B, A order, rows from the top.
const PixelRGBA8 PixelFormat = 1

type HelloStatus uint8

const (
	HelloOK              HelloStatus = 0
	HelloVersionMismatch HelloStatus = 1
	HelloBadToken        HelloStatus = 2
)

type AckStatus uint8

const (
	AckApplied   AckStatus = 0
	AckQueued    AckStatus = 1
	AckUnmatched AckStatus = 2
	AckNeedFull  AckStatus = 3
	AckRejected  AckStatus = 4
)

type AckFlags uint16

const (
	FlagResyncAll      AckFlags = 1 << 0
	FlagViewerAttached AckFlags = 1 << 1
)

type AttrState uint8

const (
	AttrAbsent    AttrState = 0
	AttrInline    AttrState = 1
	AttrUnchanged AttrState = 2
)

type AttrID uint16

const (
	AttrVertCo      AttrID = 0x0001
	AttrLoopVert    AttrID = 0x0002
	AttrLoopNormal  AttrID = 0x0003
	AttrLoopTangent AttrID = 0x0004
	AttrLoopBTSign  AttrID = 0x0005
	attrUVBase      AttrID = 0x0100
	attrColorBase   AttrID = 0x0200
	AttrVGCount     AttrID = 0x0300
	AttrVGIndex     AttrID = 0x0301
	AttrVGWeight    AttrID = 0x0302
	maxLayers              = 0x100
)

func AttrUV(layer int) AttrID    { return attrUVBase + AttrID(layer) }
func AttrColor(layer int) AttrID { return attrColorBase + AttrID(layer) }

// Hash is the client's BLAKE2b-128 content key of an attribute's bytes.
type Hash [16]byte

func (h Hash) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(h[:])), nil }

func (h *Hash) UnmarshalText(text []byte) error {
	if hex.DecodedLen(len(text)) != len(h) {
		return fmt.Errorf("hash of %d hex digits", len(text))
	}
	_, err := hex.Decode(h[:], text)
	return err
}

type Message interface {
	Type() MessageType
	encode(e *encoder)
}

type Hello struct {
	ProtocolVersion uint16 `json:"protocol_version"`
	AddonVersion    string `json:"addon_version"`
	BlenderVersion  string `json:"blender_version"`
	PID             uint32 `json:"pid"`
	Token           string `json:"token"`
}

type HelloAck struct {
	Status          HelloStatus `json:"status"`
	ProtocolVersion uint16      `json:"protocol_version"`
}

// Scene is the export setup of one Blender scene: which exporter add-on it uses, its folders and
// which objects make up each component and part.
type Scene struct {
	SceneRev        uint32 `json:"scene_rev"`
	Exporter        string `json:"exporter"`
	ExporterVersion string `json:"exporter_version"`
	Game            string `json:"game"`
	SourceFolder    string `json:"source_folder"`
	OutputFolder    string `json:"output_folder"`
	// Settings is the exporter-specific block, passed through unparsed.
	Settings   []byte      `json:"settings"`
	Components []Component `json:"components"`
	Errors     []string    `json:"errors"`
}

type Component struct {
	Name  string `json:"name"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Name    string        `json:"name"`
	Objects []SceneObject `json:"objects"`
}

type SceneObject struct {
	ObjectKey  string `json:"object_key"`
	Name       string `json:"name"`
	Collection string `json:"collection"`
}

type MeshBegin struct {
	ObjectKey    string       `json:"object_key"`
	Revision     uint32       `json:"revision"`
	SceneRev     uint32       `json:"scene_rev"`
	VertexCount  uint32       `json:"vertex_count"`
	LoopCount    uint32       `json:"loop_count"`
	VertexGroups []string     `json:"vertex_groups"`
	Attrs        []AttrHeader `json:"attrs"`
}

type AttrHeader struct {
	ID      AttrID    `json:"id"`
	State   AttrState `json:"state"`
	ByteLen uint32    `json:"byte_len"`
	Hash    Hash      `json:"hash"`
}

type MeshChunk struct {
	AttrID AttrID `json:"attr_id"`
	Offset uint32 `json:"offset"`
	Data   []byte `json:"data"`
}

type MeshCommit struct{}

// TextureBegin opens the transfer of the pixels of one Blender image. Version grows with each transfer of
// the image on a connection.
type TextureBegin struct {
	Image    string      `json:"image"`
	Version  uint32      `json:"version"`
	SceneRev uint32      `json:"scene_rev"`
	Width    uint32      `json:"width"`
	Height   uint32      `json:"height"`
	Format   PixelFormat `json:"format"`
	ByteLen  uint32      `json:"byte_len"`
}

type TextureChunk struct {
	Offset uint32 `json:"offset"`
	Data   []byte `json:"data"`
}

type TextureCommit struct{}

type Ack struct {
	Status  AckStatus `json:"status"`
	Flags   AckFlags  `json:"flags"`
	Needed  []AttrID  `json:"needed"`
	Message string    `json:"message"`
}

type Ping struct{}

type Pong struct {
	Flags AckFlags `json:"flags"`
}

type ClientError struct {
	// ObjectKey is empty for an error that concerns the whole scene.
	ObjectKey string `json:"object_key"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func (Hello) Type() MessageType         { return TypeHello }
func (HelloAck) Type() MessageType      { return TypeHelloAck }
func (Scene) Type() MessageType         { return TypeScene }
func (MeshBegin) Type() MessageType     { return TypeMeshBegin }
func (MeshChunk) Type() MessageType     { return TypeMeshChunk }
func (MeshCommit) Type() MessageType    { return TypeMeshCommit }
func (TextureBegin) Type() MessageType  { return TypeTextureBegin }
func (TextureChunk) Type() MessageType  { return TypeTextureChunk }
func (TextureCommit) Type() MessageType { return TypeTextureCommit }
func (Ack) Type() MessageType           { return TypeAck }
func (Ping) Type() MessageType          { return TypePing }
func (Pong) Type() MessageType          { return TypePong }
func (ClientError) Type() MessageType   { return TypeClientError }

func (m Hello) encode(e *encoder) {
	e.u16(m.ProtocolVersion)
	e.str(m.AddonVersion)
	e.str(m.BlenderVersion)
	e.u32(m.PID)
	e.str(m.Token)
}

func (m HelloAck) encode(e *encoder) {
	e.u8(uint8(m.Status))
	e.u16(m.ProtocolVersion)
}

func (m Scene) encode(e *encoder) {
	e.u32(m.SceneRev)
	e.str(m.Exporter)
	e.str(m.ExporterVersion)
	e.str(m.Game)
	e.str(m.SourceFolder)
	e.str(m.OutputFolder)
	e.bytes32(m.Settings)
	if e.count16(len(m.Components)) {
		for _, component := range m.Components {
			e.str(component.Name)
			if !e.count16(len(component.Parts)) {
				continue
			}
			for _, part := range component.Parts {
				e.str(part.Name)
				if !e.count16(len(part.Objects)) {
					continue
				}
				for _, object := range part.Objects {
					e.str(object.ObjectKey)
					e.str(object.Name)
					e.str(object.Collection)
				}
			}
		}
	}
	e.strs(m.Errors)
}

func (m MeshBegin) encode(e *encoder) {
	e.str(m.ObjectKey)
	e.u32(m.Revision)
	e.u32(m.SceneRev)
	e.u32(m.VertexCount)
	e.u32(m.LoopCount)
	e.strs(m.VertexGroups)
	if e.count16(len(m.Attrs)) {
		for _, attr := range m.Attrs {
			e.u16(uint16(attr.ID))
			e.u8(uint8(attr.State))
			e.u32(attr.ByteLen)
			e.raw(attr.Hash[:])
		}
	}
}

func (m MeshChunk) encode(e *encoder) {
	e.u16(uint16(m.AttrID))
	e.u32(m.Offset)
	e.bytes32(m.Data)
}

func (MeshCommit) encode(*encoder) {}

func (m TextureBegin) encode(e *encoder) {
	e.str(m.Image)
	e.u32(m.Version)
	e.u32(m.SceneRev)
	e.u32(m.Width)
	e.u32(m.Height)
	e.u8(uint8(m.Format))
	e.u32(m.ByteLen)
}

func (m TextureChunk) encode(e *encoder) {
	e.u32(m.Offset)
	e.bytes32(m.Data)
}

func (TextureCommit) encode(*encoder) {}

func (m Ack) encode(e *encoder) {
	e.u8(uint8(m.Status))
	e.u16(uint16(m.Flags))
	if e.count16(len(m.Needed)) {
		for _, id := range m.Needed {
			e.u16(uint16(id))
		}
	}
	e.str(m.Message)
}

func (Ping) encode(*encoder) {}

func (m Pong) encode(e *encoder) { e.u16(uint16(m.Flags)) }

func (m ClientError) encode(e *encoder) {
	e.str(m.ObjectKey)
	e.str(m.Code)
	e.str(m.Message)
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

// Decode parses a frame's payload. A type it does not know returns ErrUnknownType. Byte fields
// alias the frame's payload.
func Decode(frame Frame) (Message, error) {
	d := decoder{buf: frame.Payload}
	var m Message
	switch frame.Type {
	case TypeHello:
		m = Hello{
			ProtocolVersion: d.u16(),
			AddonVersion:    d.str(),
			BlenderVersion:  d.str(),
			PID:             d.u32(),
			Token:           d.str(),
		}
	case TypeHelloAck:
		m = HelloAck{Status: HelloStatus(d.u8()), ProtocolVersion: d.u16()}
	case TypeScene:
		m = decodeScene(&d)
	case TypeMeshBegin:
		m = decodeMeshBegin(&d)
	case TypeMeshChunk:
		m = MeshChunk{AttrID: AttrID(d.u16()), Offset: d.u32(), Data: d.bytes32()}
	case TypeMeshCommit:
		m = MeshCommit{}
	case TypeTextureBegin:
		m = TextureBegin{
			Image: d.str(), Version: d.u32(), SceneRev: d.u32(), Width: d.u32(), Height: d.u32(),
			Format: PixelFormat(d.u8()), ByteLen: d.u32(),
		}
	case TypeTextureChunk:
		m = TextureChunk{Offset: d.u32(), Data: d.bytes32()}
	case TypeTextureCommit:
		m = TextureCommit{}
	case TypeAck:
		status, flags := AckStatus(d.u8()), AckFlags(d.u16())
		needed := make([]AttrID, d.count16(2))
		for i := range needed {
			needed[i] = AttrID(d.u16())
		}
		m = Ack{Status: status, Flags: flags, Needed: needed, Message: d.str()}
	case TypePing:
		m = Ping{}
	case TypePong:
		m = Pong{Flags: AckFlags(d.u16())}
	case TypeClientError:
		m = ClientError{ObjectKey: d.str(), Code: d.str(), Message: d.str()}
	default:
		return nil, fmt.Errorf("%w %#04x", ErrUnknownType, uint16(frame.Type))
	}
	if err := d.finish(); err != nil {
		return nil, fmt.Errorf("decode %#04x: %w", uint16(frame.Type), err)
	}
	return m, nil
}

func decodeScene(d *decoder) Scene {
	scene := Scene{
		SceneRev: d.u32(), Exporter: d.str(), ExporterVersion: d.str(), Game: d.str(),
		SourceFolder: d.str(), OutputFolder: d.str(), Settings: d.bytes32(),
	}
	scene.Components = make([]Component, d.count16(4))
	for i := range scene.Components {
		component := Component{Name: d.str()}
		component.Parts = make([]Part, d.count16(4))
		for j := range component.Parts {
			part := Part{Name: d.str()}
			part.Objects = make([]SceneObject, d.count16(6))
			for k := range part.Objects {
				part.Objects[k] = SceneObject{ObjectKey: d.str(), Name: d.str(), Collection: d.str()}
			}
			component.Parts[j] = part
		}
		scene.Components[i] = component
	}
	scene.Errors = d.strs()
	return scene
}

func decodeMeshBegin(d *decoder) MeshBegin {
	begin := MeshBegin{
		ObjectKey: d.str(), Revision: d.u32(), SceneRev: d.u32(), VertexCount: d.u32(), LoopCount: d.u32(),
		VertexGroups: d.strs(),
	}
	begin.Attrs = make([]AttrHeader, d.count16(23))
	for i := range begin.Attrs {
		attr := AttrHeader{ID: AttrID(d.u16()), State: AttrState(d.u8()), ByteLen: d.u32()}
		copy(attr.Hash[:], d.take(len(attr.Hash)))
		begin.Attrs[i] = attr
	}
	return begin
}
