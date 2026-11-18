package blenderlink

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"slices"
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	// maxSessionBytes bounds the committed attribute bytes plus the inline or pixel bytes of the
	// transfer in progress on one connection.
	maxSessionBytes = 512 << 20
	// maxTextureDimension is the largest 2D texture Direct3D 11 creates.
	maxTextureDimension = 16384

	handshakeTimeout = 5 * time.Second
	idleTimeout      = 15 * time.Second
	writeTimeout     = 10 * time.Second
)

var (
	errProtocol        = errors.New("blender link protocol violation")
	errVersionMismatch = errors.New("blender link add-on speaks another protocol version")
	errBadToken        = errors.New("blender link client failed authentication")
)

// session serves one client connection on one goroutine, strictly one request and one reply at a
// time.
type session struct {
	manager   *Manager
	ctx       context.Context
	conn      net.Conn
	id        uint64
	clientPID uint32
	link      Link

	scene        *Scene
	sceneObjects map[string]struct{}
	objects      map[string]committedObject
	blobs        map[Hash][]byte
	held         int64
	transfer     *transfer
	texture      *textureTransfer

	viewerAttached bool
	resync         atomic.Bool
}

type committedObject struct {
	revision uint32
	attrs    map[AttrID]Hash
}

type transfer struct {
	requestID uint32
	begin     MeshBegin
	inbound   map[AttrID]*inbound
	// rejected is the first rule the transfer broke; its remaining chunks are discarded.
	rejected string
	resync   bool
	needed   []AttrID
}

type inbound struct {
	data   []byte
	filled int
	hash   Hash
}

type textureTransfer struct {
	requestID uint32
	begin     TextureBegin
	data      []byte
	filled    int
	// rejected is the first rule the transfer broke; its remaining chunks are discarded.
	rejected string
	resync   bool
}

func newSession(ctx context.Context, m *Manager, conn net.Conn, id uint64, clientPID uint32) *session {
	return &session{
		manager: m, ctx: ctx, conn: conn, id: id, clientPID: clientPID,
		sceneObjects: map[string]struct{}{}, objects: map[string]committedObject{}, blobs: map[Hash][]byte{},
	}
}

func (s *session) serve() error {
	if err := s.handshake(); err != nil {
		return err
	}
	s.manager.attach(s)
	defer s.manager.detach(s)

	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(idleTimeout))
		frame, err := ReadFrame(s.conn)
		if err != nil {
			return err
		}
		if frame.RequestID == 0 {
			return fmt.Errorf("%w: request ID 0 on %#04x", errProtocol, uint16(frame.Type))
		}
		reply, err := s.handle(frame)
		if err != nil {
			return err
		}
		if reply != nil {
			if err := s.write(frame.RequestID, reply); err != nil {
				return err
			}
		}
	}
}

func (s *session) handshake() error {
	_ = s.conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	frame, err := ReadFrame(s.conn)
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if frame.Type != TypeHello || frame.RequestID == 0 || len(frame.Payload) < 2 {
		return fmt.Errorf("%w: first frame %#04x is not a hello", errProtocol, uint16(frame.Type))
	}

	version := binary.LittleEndian.Uint16(frame.Payload)
	if version != ProtocolVersion {
		ack := HelloAck{Status: HelloVersionMismatch, ProtocolVersion: ProtocolVersion}
		return errors.Join(fmt.Errorf("%w: %d", errVersionMismatch, version), s.write(frame.RequestID, ack))
	}
	m, err := Decode(frame)
	if err != nil {
		return err
	}
	hello, ok := m.(Hello)
	if !ok {
		return fmt.Errorf("%w: hello decoded as %T", errProtocol, m)
	}
	if !tokensEqual(hello.Token, s.manager.token) || hello.PID != s.clientPID {
		ack := HelloAck{Status: HelloBadToken, ProtocolVersion: ProtocolVersion}
		return errors.Join(
			fmt.Errorf("%w: hello pid %d, pipe client pid %d", errBadToken, hello.PID, s.clientPID),
			s.write(frame.RequestID, ack),
		)
	}

	s.link = Link{
		Session: s.id, AddonVersion: hello.AddonVersion, BlenderVersion: hello.BlenderVersion, PID: s.clientPID,
	}
	return s.write(frame.RequestID, HelloAck{Status: HelloOK, ProtocolVersion: ProtocolVersion})
}

func tokensEqual(left, right string) bool {
	return right != "" && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (s *session) write(requestID uint32, m Message) error {
	frame, err := Encode(requestID, m)
	if err != nil {
		return err
	}
	data, err := AppendFrame(nil, frame)
	if err != nil {
		return err
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = s.conn.Write(data)
	return err
}

// handle returns the reply to a request, nil for a frame without one, or an error that closes the
// connection.
func (s *session) handle(frame Frame) (Message, error) {
	switch frame.Type {
	case TypeMeshChunk:
		return nil, s.chunk(frame)
	case TypeMeshCommit:
		return s.commit(frame)
	case TypeTextureChunk:
		return nil, s.textureChunk(frame)
	case TypeTextureCommit:
		return s.textureCommit(frame)
	default:
	}
	if s.transfer != nil || s.texture != nil {
		return nil, fmt.Errorf("%w: %#04x during a transfer", errProtocol, uint16(frame.Type))
	}

	switch frame.Type {
	case TypeHello:
		return nil, fmt.Errorf("%w: second hello", errProtocol)
	case TypeScene, TypeMeshBegin, TypeTextureBegin, TypePing, TypeClientError:
	default:
		return Ack{Status: AckRejected, Flags: s.flags(), Message: "unsupported message type"}, nil
	}
	m, err := Decode(frame)
	if err != nil {
		return nil, err
	}
	switch msg := m.(type) {
	case Scene:
		return s.applyScene(msg), nil
	case MeshBegin:
		s.begin(frame.RequestID, msg)
		return nil, nil
	case TextureBegin:
		s.beginTexture(frame.RequestID, msg)
		return nil, nil
	case Ping:
		return Pong{Flags: s.flags()}, nil
	case ClientError:
		s.manager.handler().ClientError(s.ctx, s.link, msg)
		return Ack{Status: AckApplied, Flags: s.flags()}, nil
	default:
		return nil, fmt.Errorf("%w: unexpected %T", errProtocol, m)
	}
}

func (s *session) flags() AckFlags {
	var flags AckFlags
	if s.viewerAttached {
		flags |= FlagViewerAttached
	}
	if s.resync.Swap(false) {
		flags |= FlagResyncAll
	}
	return flags
}

func (s *session) applyScene(scene Scene) Ack {
	keys := map[string]struct{}{}
	for _, component := range scene.Components {
		for _, part := range component.Parts {
			for _, object := range part.Objects {
				if object.ObjectKey == "" {
					return Ack{Status: AckRejected, Flags: s.flags(), Message: "empty object key"}
				}
				if _, ok := keys[object.ObjectKey]; ok {
					return Ack{
						Status:  AckRejected,
						Flags:   s.flags(),
						Message: fmt.Sprintf("object key %q repeats", object.ObjectKey),
					}
				}
				keys[object.ObjectKey] = struct{}{}
			}
		}
	}

	result, err := s.manager.handler().Scene(s.ctx, s.link, scene)
	if err != nil {
		s.manager.reportHandlerError(err, "BlenderLink.Scene", map[string]any{
			"session":       s.id,
			"scene_rev":     scene.SceneRev,
			"exporter":      scene.Exporter,
			"output_folder": scene.OutputFolder,
		})
		return Ack{Status: AckRejected, Flags: s.flags(), Message: err.Error()}
	}

	s.scene, s.sceneObjects = &scene, keys
	for key := range s.objects {
		if _, ok := keys[key]; !ok {
			delete(s.objects, key)
		}
	}
	s.collect()
	s.viewerAttached = result.ViewerAttached
	s.manager.publish(s)
	return s.ack(result)
}

func (s *session) begin(requestID uint32, begin MeshBegin) {
	t := &transfer{requestID: requestID, begin: begin}
	s.transfer = t
	reject := func(format string, args ...any) {
		if t.rejected == "" {
			t.rejected = fmt.Sprintf(format, args...)
		}
	}

	if s.scene == nil || begin.SceneRev != s.scene.SceneRev {
		t.resync = true
		reject("scene revision %d is not the current scene", begin.SceneRev)
		return
	}
	if _, ok := s.sceneObjects[begin.ObjectKey]; !ok {
		t.resync = true
		reject("object %q is not in the scene", begin.ObjectKey)
		return
	}
	if begin.LoopCount%3 != 0 {
		reject("%d loops do not form triangles", begin.LoopCount)
		return
	}

	seen := map[AttrID]struct{}{}
	var inlineBytes int64
	for _, attr := range begin.Attrs {
		layout, ok := layoutOf(attr.ID)
		if !ok {
			reject("unknown attribute %#04x", uint16(attr.ID))
			continue
		}
		if _, ok := seen[attr.ID]; ok {
			reject("attribute %#04x repeats", uint16(attr.ID))
			continue
		}
		seen[attr.ID] = struct{}{}

		switch attr.State {
		case AttrAbsent:
			if attr.ByteLen != 0 {
				reject("absent attribute %#04x has %d bytes", uint16(attr.ID), attr.ByteLen)
			}
			continue
		case AttrInline, AttrUnchanged:
		default:
			reject("attribute %#04x has state %d", uint16(attr.ID), attr.State)
			continue
		}
		if err := layout.checkLen(attr.ByteLen, begin.VertexCount, begin.LoopCount); err != nil {
			reject("attribute %#04x: %v", uint16(attr.ID), err)
			continue
		}
		if attr.State == AttrInline {
			inlineBytes += int64(attr.ByteLen)
			continue
		}
		blob, ok := s.blobs[attr.Hash]
		switch {
		case !ok:
			t.needed = append(t.needed, attr.ID)
		case len(blob) != int(attr.ByteLen):
			reject("attribute %#04x has %d bytes, the held content %d", uint16(attr.ID), attr.ByteLen, len(blob))
		}
	}
	if t.rejected != "" {
		return
	}
	if s.held+inlineBytes > maxSessionBytes {
		reject(
			"%d held and %d new bytes exceed the %d MiB connection limit",
			s.held, inlineBytes, maxSessionBytes>>20,
		)
		return
	}
	if len(t.needed) > 0 {
		return
	}

	t.inbound = map[AttrID]*inbound{}
	for _, attr := range begin.Attrs {
		if attr.State == AttrInline {
			t.inbound[attr.ID] = &inbound{data: make([]byte, attr.ByteLen), hash: attr.Hash}
		}
	}
}

func (s *session) chunk(frame Frame) error {
	t := s.transfer
	if t == nil || frame.RequestID != t.requestID {
		return fmt.Errorf("%w: mesh chunk outside its transfer", errProtocol)
	}
	m, err := Decode(frame)
	if err != nil {
		return err
	}
	chunk, ok := m.(MeshChunk)
	if !ok {
		return fmt.Errorf("%w: mesh chunk decoded as %T", errProtocol, m)
	}
	if t.rejected != "" || len(t.needed) > 0 {
		return nil
	}

	fail := func(format string, args ...any) {
		t.rejected = fmt.Sprintf(format, args...)
		t.inbound = nil
	}
	in := t.inbound[chunk.AttrID]
	switch {
	case in == nil:
		fail("chunk for attribute %#04x, which is not inline", uint16(chunk.AttrID))
	case len(chunk.Data) > MaxChunkData:
		fail("chunk of %d bytes", len(chunk.Data))
	case int(chunk.Offset) != in.filled:
		fail("attribute %#04x chunk at %d, expected %d", uint16(chunk.AttrID), chunk.Offset, in.filled)
	case in.filled+len(chunk.Data) > len(in.data):
		fail("attribute %#04x chunks pass its %d bytes", uint16(chunk.AttrID), len(in.data))
	default:
		in.filled += copy(in.data[in.filled:], chunk.Data)
	}
	return nil
}

func (s *session) commit(frame Frame) (Message, error) {
	t := s.transfer
	if t == nil || frame.RequestID != t.requestID {
		return nil, fmt.Errorf("%w: mesh commit outside its transfer", errProtocol)
	}
	if _, err := Decode(frame); err != nil {
		return nil, err
	}
	s.transfer = nil
	return s.apply(t), nil
}

// apply answers a completed transfer and, unless it is rejected or needs full data, hands the object
// to the handler and keeps its attribute bytes.
func (s *session) apply(t *transfer) Ack {
	if t.rejected != "" {
		flags := s.flags()
		if t.resync {
			flags |= FlagResyncAll
		}
		return Ack{Status: AckRejected, Flags: flags, Message: t.rejected}
	}
	if len(t.needed) > 0 {
		return Ack{Status: AckNeedFull, Flags: s.flags(), Needed: t.needed}
	}

	data := map[AttrID][]byte{}
	hashes := map[AttrID]Hash{}
	for _, attr := range t.begin.Attrs {
		switch attr.State {
		case AttrInline:
			in := t.inbound[attr.ID]
			if in.filled != len(in.data) {
				return Ack{
					Status: AckRejected,
					Flags:  s.flags(),
					Message: fmt.Sprintf(
						"attribute %#04x has %d of %d bytes",
						uint16(attr.ID),
						in.filled,
						len(in.data),
					),
				}
			}
			data[attr.ID] = in.data
		case AttrUnchanged:
			data[attr.ID] = s.blobs[attr.Hash]
		case AttrAbsent:
			continue
		default:
		}
		hashes[attr.ID] = attr.Hash
	}
	update, err := buildUpdate(t.begin, data)
	if err != nil {
		return Ack{Status: AckRejected, Flags: s.flags(), Message: err.Error()}
	}
	previous := s.objects[t.begin.ObjectKey]
	for id, hash := range hashes {
		if old, ok := previous.attrs[id]; !ok || old != hash {
			update.Changed = append(update.Changed, id)
		}
	}
	slices.Sort(update.Changed)

	result, err := s.manager.handler().Object(s.ctx, s.link, update)
	if err != nil {
		s.manager.reportHandlerError(err, "BlenderLink.Object", map[string]any{
			"session": s.id, "object_key": t.begin.ObjectKey, "revision": t.begin.Revision,
			"scene_rev": t.begin.SceneRev, "output_folder": s.scene.OutputFolder,
		})
		return Ack{Status: AckRejected, Flags: s.flags(), Message: err.Error()}
	}

	for _, in := range t.inbound {
		s.blobs[in.hash] = in.data
	}
	s.objects[t.begin.ObjectKey] = committedObject{revision: t.begin.Revision, attrs: hashes}
	s.collect()
	s.viewerAttached = result.ViewerAttached
	return s.ack(result)
}

func (s *session) beginTexture(requestID uint32, begin TextureBegin) {
	t := &textureTransfer{requestID: requestID, begin: begin}
	s.texture = t
	want := uint64(begin.Width) * uint64(begin.Height) * 4
	switch {
	case s.scene == nil || begin.SceneRev != s.scene.SceneRev:
		t.resync = true
		t.rejected = fmt.Sprintf("scene revision %d is not the current scene", begin.SceneRev)
	case begin.Image == "":
		t.rejected = "empty image name"
	case begin.Format != PixelRGBA8:
		t.rejected = fmt.Sprintf("image %q has pixel format %d", begin.Image, begin.Format)
	case begin.Width == 0 || begin.Height == 0 || begin.Width > maxTextureDimension ||
		begin.Height > maxTextureDimension:
		t.rejected = fmt.Sprintf("image %q is %dx%d", begin.Image, begin.Width, begin.Height)
	case uint64(begin.ByteLen) != want:
		t.rejected = fmt.Sprintf("image %q has %d bytes, want %d for %dx%d RGBA8",
			begin.Image, begin.ByteLen, want, begin.Width, begin.Height)
	case s.held+int64(begin.ByteLen) > maxSessionBytes:
		t.rejected = fmt.Sprintf("%d held and %d new bytes exceed the %d MiB connection limit",
			s.held, begin.ByteLen, maxSessionBytes>>20)
	default:
		t.data = make([]byte, begin.ByteLen)
	}
}

func (s *session) textureChunk(frame Frame) error {
	t := s.texture
	if t == nil || frame.RequestID != t.requestID {
		return fmt.Errorf("%w: texture chunk outside its transfer", errProtocol)
	}
	m, err := Decode(frame)
	if err != nil {
		return err
	}
	chunk, ok := m.(TextureChunk)
	if !ok {
		return fmt.Errorf("%w: texture chunk decoded as %T", errProtocol, m)
	}
	if t.rejected != "" {
		return nil
	}

	fail := func(format string, args ...any) {
		t.rejected = fmt.Sprintf(format, args...)
		t.data = nil
	}
	switch {
	case len(chunk.Data) > MaxChunkData:
		fail("chunk of %d bytes", len(chunk.Data))
	case int(chunk.Offset) != t.filled:
		fail("image %q chunk at %d, expected %d", t.begin.Image, chunk.Offset, t.filled)
	case t.filled+len(chunk.Data) > len(t.data):
		fail("image %q chunks pass its %d bytes", t.begin.Image, len(t.data))
	default:
		t.filled += copy(t.data[t.filled:], chunk.Data)
	}
	return nil
}

func (s *session) textureCommit(frame Frame) (Message, error) {
	t := s.texture
	if t == nil || frame.RequestID != t.requestID {
		return nil, fmt.Errorf("%w: texture commit outside its transfer", errProtocol)
	}
	if _, err := Decode(frame); err != nil {
		return nil, err
	}
	s.texture = nil
	return s.applyTexture(t), nil
}

// applyTexture answers a completed texture transfer and, unless it is rejected, hands the pixels to the
// handler. The session keeps none of them.
func (s *session) applyTexture(t *textureTransfer) Ack {
	if t.rejected != "" {
		flags := s.flags()
		if t.resync {
			flags |= FlagResyncAll
		}
		return Ack{Status: AckRejected, Flags: flags, Message: t.rejected}
	}
	if t.filled != len(t.data) {
		return Ack{
			Status:  AckRejected,
			Flags:   s.flags(),
			Message: fmt.Sprintf("image %q has %d of %d bytes", t.begin.Image, t.filled, len(t.data)),
		}
	}

	update := TextureUpdate{
		SceneRev: t.begin.SceneRev, Image: t.begin.Image, Version: t.begin.Version,
		Width: int(t.begin.Width), Height: int(t.begin.Height), Pixels: t.data,
	}
	result, err := s.manager.handler().Texture(s.ctx, s.link, update)
	if err != nil {
		s.manager.reportHandlerError(err, "BlenderLink.Texture", map[string]any{
			"session": s.id, "image": t.begin.Image, "version": t.begin.Version, "scene_rev": t.begin.SceneRev,
			"width": t.begin.Width, "height": t.begin.Height,
		})
		return Ack{Status: AckRejected, Flags: s.flags(), Message: err.Error()}
	}
	s.viewerAttached = result.ViewerAttached
	return s.ack(result)
}

// collect drops attribute bytes that no current object refers to.
func (s *session) collect() {
	referenced := map[Hash]struct{}{}
	for _, object := range s.objects {
		for _, hash := range object.attrs {
			referenced[hash] = struct{}{}
		}
	}
	maps.DeleteFunc(s.blobs, func(hash Hash, _ []byte) bool {
		_, ok := referenced[hash]
		return !ok
	})
	s.held = 0
	for _, blob := range s.blobs {
		s.held += int64(len(blob))
	}
}

func (s *session) ack(result Result) Ack {
	switch result.Status {
	case AckApplied, AckQueued, AckUnmatched:
		return Ack{Status: result.Status, Flags: s.flags(), Message: result.Message}
	case AckNeedFull, AckRejected:
		return Ack{Status: AckRejected, Flags: s.flags(), Message: result.Message}
	default:
		return Ack{Status: AckRejected, Flags: s.flags(), Message: fmt.Sprintf("handler status %d", result.Status)}
	}
}

func (s *session) state() LinkState {
	state := LinkState{Link: s.link, ViewerAttached: s.viewerAttached, Objects: len(s.sceneObjects)}
	if s.scene != nil {
		state.SceneRev = s.scene.SceneRev
		state.Exporter = s.scene.Exporter
		state.Game = s.scene.Game
		state.OutputFolder = s.scene.OutputFolder
		state.Errors = slices.Clone(s.scene.Errors)
	}
	return state
}

func isDisconnect(err error) bool {
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrClosed) ||
		infra.IsCancellationError(err) ||
		isPipeDisconnect(err)
}
