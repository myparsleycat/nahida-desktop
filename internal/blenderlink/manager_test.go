package blenderlink

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"

	"nahida.live/desktop/internal/appdata"
)

type recordingHandler struct {
	mu           sync.Mutex
	result       Result
	scenes       []Scene
	updates      []ObjectUpdate
	textures     []TextureUpdate
	errors       []ClientError
	disconnected chan Link
}

func newRecordingHandler(result Result) *recordingHandler {
	return &recordingHandler{result: result, disconnected: make(chan Link, 8)}
}

func (h *recordingHandler) Scene(_ context.Context, _ Link, scene Scene) (Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scenes = append(h.scenes, scene)
	return h.result, nil
}

func (h *recordingHandler) Object(_ context.Context, _ Link, update ObjectUpdate) (Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updates = append(h.updates, update)
	return h.result, nil
}

func (h *recordingHandler) Texture(_ context.Context, _ Link, update TextureUpdate) (Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.textures = append(h.textures, update)
	return h.result, nil
}

func (h *recordingHandler) textureUpdates() []TextureUpdate {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.textures)
}

func (h *recordingHandler) ClientError(_ context.Context, _ Link, report ClientError) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.errors = append(h.errors, report)
}

func (h *recordingHandler) Disconnected(link Link) { h.disconnected <- link }

func (h *recordingHandler) lastUpdate(t *testing.T) ObjectUpdate {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.updates) == 0 {
		t.Fatal("no object update reached the handler")
	}
	return h.updates[len(h.updates)-1]
}

func (h *recordingHandler) updateCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.updates)
}

func startManager(t *testing.T, handler Handler) (*Manager, *appdata.Store, discovery) {
	t.Helper()
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(Options{AppData: store, Handler: handler})
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Shutdown)

	data, err := store.ReadFile(DiscoveryFile)
	if err != nil {
		t.Fatal(err)
	}
	var found discovery
	if err := json.Unmarshal(data, &found); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(found.Pipe, pipePrefix) || found.PID != os.Getpid() || found.Proto != ProtocolVersion ||
		found.Token == "" {
		t.Fatalf("discovery file %+v", found)
	}
	return m, store, found
}

type testClient struct {
	t      *testing.T
	conn   net.Conn
	nextID uint32
}

func dial(t *testing.T, found discovery) *testClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := winio.DialPipeContext(ctx, found.Pipe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &testClient{t: t, conn: conn}
}

func connect(t *testing.T, found discovery) *testClient {
	t.Helper()
	c := dial(t, found)
	ack := c.request(
		Hello{ProtocolVersion: ProtocolVersion, AddonVersion: "0.1.0", PID: uint32(os.Getpid()), Token: found.Token},
	)
	if ack != (HelloAck{Status: HelloOK, ProtocolVersion: ProtocolVersion}) {
		t.Fatalf("hello answered %#v", ack)
	}
	return c
}

func (c *testClient) send(requestID uint32, m Message) {
	c.t.Helper()
	frame, err := Encode(requestID, m)
	if err != nil {
		c.t.Fatal(err)
	}
	data, err := AppendFrame(nil, frame)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.conn.Write(data); err != nil {
		c.t.Fatal(err)
	}
}

func (c *testClient) receive(requestID uint32) Message {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	frame, err := ReadFrame(c.conn)
	if err != nil {
		c.t.Fatal(err)
	}
	if frame.RequestID != requestID {
		c.t.Fatalf("reply to %d, want %d", frame.RequestID, requestID)
	}
	m, err := Decode(frame)
	if err != nil {
		c.t.Fatal(err)
	}
	return m
}

func (c *testClient) request(m Message) Message {
	c.t.Helper()
	c.nextID++
	c.send(c.nextID, m)
	return c.receive(c.nextID)
}

func (c *testClient) ack(m Message) Ack {
	c.t.Helper()
	ack, ok := c.request(m).(Ack)
	if !ok {
		c.t.Fatalf("%T answered with something else than an ack", m)
	}
	return ack
}

// sendMesh streams the inline attributes in chunks of at most chunkSize bytes, alternating between
// attributes so their chunks interleave.
func (c *testClient) sendMesh(begin MeshBegin, data map[AttrID][]byte, chunkSize int) Ack {
	c.t.Helper()
	c.nextID++
	c.send(c.nextID, begin)
	offsets := map[AttrID]int{}
	for sent := true; sent; {
		sent = false
		for _, attr := range begin.Attrs {
			bytes := data[attr.ID]
			offset := offsets[attr.ID]
			if attr.State != AttrInline || offset >= len(bytes) {
				continue
			}
			end := min(offset+chunkSize, len(bytes))
			c.send(c.nextID, MeshChunk{AttrID: attr.ID, Offset: uint32(offset), Data: bytes[offset:end]})
			offsets[attr.ID], sent = end, true
		}
	}
	c.send(c.nextID, MeshCommit{})
	ack, ok := c.receive(c.nextID).(Ack)
	if !ok {
		c.t.Fatal("mesh commit answered with something else than an ack")
	}
	return ack
}

// contentKey stands in for the add-on's BLAKE2b; the server treats hashes as opaque keys.
func contentKey(data []byte) Hash {
	sum := sha256.Sum256(data)
	return Hash(sum[:16])
}

func attr(id AttrID, state AttrState, data []byte) AttrHeader {
	return AttrHeader{ID: id, State: state, ByteLen: uint32(len(data)), Hash: contentKey(data)}
}

func f32Bytes(values ...float32) []byte {
	var b []byte
	for _, v := range values {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	return b
}

func u32Bytes(values ...uint32) []byte {
	var b []byte
	for _, v := range values {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}

func u16Bytes(values ...uint16) []byte {
	var b []byte
	for _, v := range values {
		b = binary.LittleEndian.AppendUint16(b, v)
	}
	return b
}

var testScene = Scene{
	SceneRev: 1, Exporter: "xxmi", Game: "GIMI", OutputFolder: `D:\Mods\Test`,
	Components: []Component{{Name: "Body", Parts: []Part{{Name: "BodyA", Objects: []SceneObject{
		{ObjectKey: "a", Name: "BodyA"}, {ObjectKey: "b", Name: "BodyA.Extra"},
	}}}}},
}

type triangle struct {
	positions, loops, normals, uv1, counts, indices, weights []byte
}

func newTriangle(x float32) triangle {
	return triangle{
		positions: f32Bytes(x, 0, 0, 1, 0, 0, 0, 1, 0),
		loops:     u32Bytes(0, 1, 2),
		normals:   f32Bytes(0, 0, 1, 0, 0, 1, 0, 0, 1),
		uv1:       f32Bytes(0, 0, 1, 0, 0, 1),
		counts:    []byte{1, 2, 0},
		indices:   u16Bytes(1, 0, 1),
		weights:   f32Bytes(1, 0.25, 0.75),
	}
}

func (tr triangle) data() map[AttrID][]byte {
	return map[AttrID][]byte{
		AttrVertCo: tr.positions, AttrLoopVert: tr.loops, AttrLoopNormal: tr.normals, AttrUV(1): tr.uv1,
		AttrVGCount: tr.counts, AttrVGIndex: tr.indices, AttrVGWeight: tr.weights,
	}
}

func (tr triangle) begin(revision uint32, unchanged ...AttrID) MeshBegin {
	state := func(id AttrID) AttrState {
		if slices.Contains(unchanged, id) {
			return AttrUnchanged
		}
		return AttrInline
	}
	data := tr.data()
	begin := MeshBegin{
		ObjectKey: "a", Revision: revision, SceneRev: testScene.SceneRev, VertexCount: 3, LoopCount: 3,
		VertexGroups: []string{"0", "1"},
	}
	for _, id := range []AttrID{AttrVertCo, AttrLoopVert, AttrLoopNormal, AttrUV(1), AttrVGCount, AttrVGIndex, AttrVGWeight} {
		begin.Attrs = append(begin.Attrs, attr(id, state(id), data[id]))
	}
	return begin
}

func TestManagerStreamsObjectRevisions(t *testing.T) {
	handler := newRecordingHandler(Result{Status: AckApplied, ViewerAttached: true})
	m, _, found := startManager(t, handler)
	c := connect(t, found)

	if ack := c.ack(testScene); ack.Status != AckApplied || ack.Flags != FlagViewerAttached {
		t.Fatalf("scene answered %+v", ack)
	}
	first := newTriangle(0)
	if ack := c.sendMesh(first.begin(1), first.data(), 5); ack.Status != AckApplied {
		t.Fatalf("first revision answered %+v", ack)
	}
	got := handler.lastUpdate(t)
	want := ObjectUpdate{
		SceneRev: 1, ObjectKey: "a", Revision: 1, VertexCount: 3, LoopCount: 3, VertexGroups: []string{"0", "1"},
		Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, LoopVertices: []uint32{0, 1, 2},
		Normals: []float32{0, 0, 1, 0, 0, 1, 0, 0, 1}, UVs: []Layer{{Index: 1, Data: []float32{0, 0, 1, 0, 0, 1}}},
		Weights: Weights{Counts: []uint8{1, 2, 0}, Groups: []uint16{1, 0, 1}, Values: []float32{1, 0.25, 0.75}},
		Changed: []AttrID{AttrVertCo, AttrLoopVert, AttrLoopNormal, AttrUV(1), AttrVGCount, AttrVGIndex, AttrVGWeight},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first update\n got %+v\nwant %+v", got, want)
	}

	// Only the positions move; the rest refers to the bytes the server kept from revision 1.
	second := newTriangle(0.5)
	unchanged := []AttrID{AttrLoopVert, AttrLoopNormal, AttrUV(1), AttrVGCount, AttrVGIndex, AttrVGWeight}
	if ack := c.sendMesh(
		second.begin(2, unchanged...),
		map[AttrID][]byte{AttrVertCo: second.positions},
		1<<20,
	); ack.Status != AckApplied {
		t.Fatalf("second revision answered %+v", ack)
	}
	got = handler.lastUpdate(t)
	want.Revision, want.Positions, want.Changed = 2, []float32{0.5, 0, 0, 1, 0, 0, 0, 1, 0}, []AttrID{AttrVertCo}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("second update\n got %+v\nwant %+v", got, want)
	}

	state := m.State()
	if !state.Listening || len(state.Links) != 1 || state.Links[0].Objects != 2 || !state.Links[0].ViewerAttached ||
		state.Links[0].PID != uint32(os.Getpid()) {
		t.Fatalf("state %+v", state)
	}
}

func TestManagerAnswersNeedFullForUnknownHash(t *testing.T) {
	handler := newRecordingHandler(Result{Status: AckQueued})
	_, _, found := startManager(t, handler)
	c := connect(t, found)
	c.ack(testScene)

	tr := newTriangle(0)
	ack := c.sendMesh(tr.begin(1, AttrLoopNormal, AttrUV(1)), tr.data(), 1<<20)
	if ack.Status != AckNeedFull || !reflect.DeepEqual(ack.Needed, []AttrID{AttrLoopNormal, AttrUV(1)}) {
		t.Fatalf("unknown hashes answered %+v", ack)
	}
	if handler.updateCount() != 0 {
		t.Fatal("a transfer that needs full data reached the handler")
	}
	if ack := c.sendMesh(tr.begin(1), tr.data(), 1<<20); ack.Status != AckQueued {
		t.Fatalf("full resend answered %+v", ack)
	}
}

func TestManagerRejectsTransfersAndKeepsConnection(t *testing.T) {
	handler := newRecordingHandler(Result{Status: AckApplied})
	_, _, found := startManager(t, handler)
	c := connect(t, found)
	c.ack(testScene)

	tr := newTriangle(0)
	cases := []struct {
		name   string
		begin  func() MeshBegin
		data   map[AttrID][]byte
		resync bool
		reason string
	}{
		{
			name:   "length disagrees with vertex count",
			reason: "want 48 for 4 vertices",
			begin: func() MeshBegin {
				b := tr.begin(1)
				b.VertexCount = 4
				return b
			},
		},
		{
			name:   "connection byte limit",
			reason: "connection limit",
			begin: func() MeshBegin {
				b := tr.begin(1)
				b.VertexCount = maxSessionBytes/12 + 1
				b.Attrs = []AttrHeader{{ID: AttrVertCo, State: AttrInline, ByteLen: b.VertexCount * 12}}
				return b
			},
		},
		{
			name:   "loop refers to missing vertex",
			reason: "refers to vertex 3",
			begin: func() MeshBegin {
				b := tr.begin(1)
				b.Attrs[1] = attr(AttrLoopVert, AttrInline, u32Bytes(0, 1, 3))
				return b
			},
			data: map[AttrID][]byte{AttrLoopVert: u32Bytes(0, 1, 3)},
		},
		{
			name:   "weights refer to missing group",
			reason: "vertex group 1 of 1",
			begin: func() MeshBegin {
				b := tr.begin(1)
				b.VertexGroups = b.VertexGroups[:1]
				return b
			},
		},
		{
			name:   "stale scene revision",
			reason: "not the current scene",
			begin: func() MeshBegin {
				b := tr.begin(1)
				b.SceneRev = 9
				return b
			},
			resync: true,
		},
		{
			name:   "repeated attribute",
			reason: "repeats",
			begin: func() MeshBegin {
				b := tr.begin(1)
				b.Attrs = append(b.Attrs, b.Attrs[0])
				return b
			},
		},
	}
	for _, tc := range cases {
		data := tr.data()
		for id, bytes := range tc.data {
			data[id] = bytes
		}
		ack := c.sendMesh(tc.begin(), data, 4)
		if ack.Status != AckRejected || !strings.Contains(ack.Message, tc.reason) ||
			(ack.Flags&FlagResyncAll != 0) != tc.resync {
			t.Fatalf("%s answered %+v", tc.name, ack)
		}
	}
	if handler.updateCount() != 0 {
		t.Fatal("a rejected transfer reached the handler")
	}

	// Chunks out of order are rejected at commit.
	c.nextID++
	begin := tr.begin(1)
	c.send(c.nextID, begin)
	c.send(c.nextID, MeshChunk{AttrID: AttrVertCo, Offset: 4, Data: tr.positions[4:]})
	c.send(c.nextID, MeshCommit{})
	if ack, ok := c.receive(c.nextID).(Ack); !ok || ack.Status != AckRejected {
		t.Fatalf("out of order chunk answered %+v", ack)
	}

	if ack := c.sendMesh(tr.begin(1), tr.data(), 4); ack.Status != AckApplied {
		t.Fatalf("valid transfer after rejections answered %+v", ack)
	}
}

// sendTexture streams pixels in chunks of at most chunkSize bytes.
func (c *testClient) sendTexture(begin TextureBegin, pixels []byte, chunkSize int) Ack {
	c.t.Helper()
	c.nextID++
	c.send(c.nextID, begin)
	for offset := 0; offset < len(pixels); offset += chunkSize {
		end := min(offset+chunkSize, len(pixels))
		c.send(c.nextID, TextureChunk{Offset: uint32(offset), Data: pixels[offset:end]})
	}
	c.send(c.nextID, TextureCommit{})
	ack, ok := c.receive(c.nextID).(Ack)
	if !ok {
		c.t.Fatal("texture commit answered with something else than an ack")
	}
	return ack
}

func TestManagerStreamsTextures(t *testing.T) {
	handler := newRecordingHandler(Result{Status: AckApplied, ViewerAttached: true})
	_, _, found := startManager(t, handler)
	c := connect(t, found)
	c.ack(testScene)

	pixels := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	begin := TextureBegin{
		Image: "Diffuse", Version: 1, SceneRev: testScene.SceneRev, Width: 2, Height: 2, Format: PixelRGBA8,
		ByteLen: uint32(len(pixels)),
	}
	if ack := c.sendTexture(begin, pixels, 6); ack.Status != AckApplied || ack.Flags != FlagViewerAttached {
		t.Fatalf("texture answered %+v", ack)
	}
	want := []TextureUpdate{{SceneRev: 1, Image: "Diffuse", Version: 1, Width: 2, Height: 2, Pixels: pixels}}
	if got := handler.textureUpdates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("texture updates\n got %+v\nwant %+v", got, want)
	}

	cases := []struct {
		name   string
		begin  func() TextureBegin
		resync bool
		reason string
	}{
		{"length disagrees with size", func() TextureBegin { b := begin; b.Height = 3; return b }, false, "want 24"},
		{"stale scene revision", func() TextureBegin { b := begin; b.SceneRev = 9; return b }, true, "current scene"},
		{"unknown pixel format", func() TextureBegin { b := begin; b.Format = 7; return b }, false, "pixel format"},
		{"connection byte limit", func() TextureBegin {
			b := begin
			b.Width, b.Height = maxTextureDimension, maxTextureDimension/2+1
			b.ByteLen = b.Width * b.Height * 4
			return b
		}, false, "connection limit"},
	}
	for _, tc := range cases {
		ack := c.sendTexture(tc.begin(), pixels, 6)
		if ack.Status != AckRejected || !strings.Contains(ack.Message, tc.reason) ||
			(ack.Flags&FlagResyncAll != 0) != tc.resync {
			t.Fatalf("%s answered %+v", tc.name, ack)
		}
	}

	// Chunks out of order are rejected at commit.
	c.nextID++
	c.send(c.nextID, begin)
	c.send(c.nextID, TextureChunk{Offset: 8, Data: pixels[8:]})
	c.send(c.nextID, TextureCommit{})
	if ack, ok := c.receive(c.nextID).(Ack); !ok || ack.Status != AckRejected {
		t.Fatalf("out of order chunk answered %+v", ack)
	}
	if got := handler.textureUpdates(); len(got) != 1 {
		t.Fatalf("%d texture updates after rejections, want 1", len(got))
	}

	// Any other request inside a texture transfer breaks the protocol and closes the connection.
	c.nextID++
	c.send(c.nextID, begin)
	c.send(c.nextID, Ping{})
	if _, err := ReadFrame(c.conn); err == nil {
		t.Fatal("connection stayed open after a ping inside a texture transfer")
	}
}

func TestManagerHandshakeFailures(t *testing.T) {
	_, _, found := startManager(t, newRecordingHandler(Result{Status: AckApplied}))
	cases := []struct {
		name  string
		hello Hello
		want  HelloStatus
	}{
		{"bad token", Hello{ProtocolVersion: ProtocolVersion, PID: uint32(os.Getpid()), Token: "wrong"}, HelloBadToken},
		{"wrong pid", Hello{ProtocolVersion: ProtocolVersion, PID: 1, Token: found.Token}, HelloBadToken},
		{"version mismatch", Hello{ProtocolVersion: ProtocolVersion + 1, Token: found.Token}, HelloVersionMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := dial(t, found)
			if ack := c.request(tc.hello); ack != (HelloAck{Status: tc.want, ProtocolVersion: ProtocolVersion}) {
				t.Fatalf("hello answered %#v", ack)
			}
			if _, err := ReadFrame(c.conn); !errors.Is(err, io.EOF) {
				t.Fatalf("connection after a failed hello: %v, want EOF", err)
			}
		})
	}
}

func TestManagerFlagsAndOtherRequests(t *testing.T) {
	handler := newRecordingHandler(Result{Status: AckApplied})
	m, _, found := startManager(t, handler)
	c := connect(t, found)

	if pong := c.request(Ping{}); pong != (Pong{}) {
		t.Fatalf("ping answered %#v", pong)
	}
	m.RequestResync(0)
	if pong := c.request(Ping{}); pong != (Pong{Flags: FlagResyncAll}) {
		t.Fatalf("ping after resync request answered %#v", pong)
	}
	if pong := c.request(Ping{}); pong != (Pong{}) {
		t.Fatalf("resync flag was not cleared: %#v", pong)
	}
	if ack := c.ack(ClientError{Code: "MISSING_UV_LAYER"}); ack.Status != AckApplied {
		t.Fatalf("client error answered %+v", ack)
	}

	c.nextID++
	frame, err := AppendFrame(nil, Frame{Type: 0x40, RequestID: c.nextID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	if ack, ok := c.receive(c.nextID).(Ack); !ok || ack.Status != AckRejected {
		t.Fatalf("unknown message type answered %+v", ack)
	}
}

func TestManagerShutdownClosesLinks(t *testing.T) {
	handler := newRecordingHandler(Result{Status: AckApplied})
	m, store, found := startManager(t, handler)
	c := connect(t, found)
	c.request(Ping{})

	m.Shutdown()
	select {
	case <-handler.disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not told about the disconnect")
	}
	if _, err := ReadFrame(c.conn); err == nil {
		t.Fatal("connection stayed open after shutdown")
	}
	path, err := store.Resolve(DiscoveryFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discovery file after shutdown: %v", err)
	}
	if m.State().Listening {
		t.Fatal("state still listening after shutdown")
	}
}
