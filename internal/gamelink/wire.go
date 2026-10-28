package gamelink

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

type encoder struct {
	buf []byte
	err error
}

func (e *encoder) u8(v uint8)   { e.buf = append(e.buf, v) }
func (e *encoder) u16(v uint16) { e.buf = binary.LittleEndian.AppendUint16(e.buf, v) }
func (e *encoder) u32(v uint32) { e.buf = binary.LittleEndian.AppendUint32(e.buf, v) }
func (e *encoder) u64(v uint64) { e.buf = binary.LittleEndian.AppendUint64(e.buf, v) }
func (e *encoder) i32(v int32)  { e.u32(uint32(v)) }
func (e *encoder) f32(v float32) {
	e.u32(math.Float32bits(v))
}

func (e *encoder) bool(v bool) {
	if v {
		e.u8(1)
		return
	}
	e.u8(0)
}

func (e *encoder) str(v string) {
	if len(v) > math.MaxUint16 || !utf8.ValidString(v) {
		e.fail("string of %d bytes", len(v))
		return
	}
	e.u16(uint16(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *encoder) count8(n int)  { e.countOf(n, math.MaxUint8, func() { e.u8(uint8(n)) }) }
func (e *encoder) count16(n int) { e.countOf(n, math.MaxUint16, func() { e.u16(uint16(n)) }) }

func (e *encoder) countOf(n, limit int, write func()) {
	if n > limit {
		e.fail("%d elements", n)
		return
	}
	write()
}

func (e *encoder) fail(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf("%w: "+format, append([]any{ErrMalformed}, args...)...)
	}
}

// decoder reads a payload; the first short read or invalid value sticks and
// every later read returns zero.
type decoder struct {
	buf []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if len(d.buf) < n {
		d.fail("payload ends early")
		return nil
	}
	v := d.buf[:n]
	d.buf = d.buf[n:]
	return v
}

func (d *decoder) u8() uint8 {
	if v := d.take(1); v != nil {
		return v[0]
	}
	return 0
}

func (d *decoder) u16() uint16 {
	if v := d.take(2); v != nil {
		return binary.LittleEndian.Uint16(v)
	}
	return 0
}

func (d *decoder) u32() uint32 {
	if v := d.take(4); v != nil {
		return binary.LittleEndian.Uint32(v)
	}
	return 0
}

func (d *decoder) u64() uint64 {
	if v := d.take(8); v != nil {
		return binary.LittleEndian.Uint64(v)
	}
	return 0
}

func (d *decoder) i32() int32   { return int32(d.u32()) }
func (d *decoder) f32() float32 { return math.Float32frombits(d.u32()) }

func (d *decoder) bool() bool {
	switch v := d.u8(); v {
	case 0:
		return false
	case 1:
		return true
	default:
		d.fail("bool %d", v)
		return false
	}
}

func (d *decoder) str() string {
	v := d.take(int(d.u16()))
	if d.err != nil {
		return ""
	}
	if !utf8.Valid(v) {
		d.fail("invalid UTF-8")
		return ""
	}
	return string(v)
}

// count reads an element count and bounds it by the bytes left, so a corrupt
// count cannot make the caller allocate far more than the frame holds.
func (d *decoder) count(n, minElementSize int) int {
	if d.err == nil && n*minElementSize > len(d.buf) {
		d.fail("%d elements in %d bytes", n, len(d.buf))
	}
	if d.err != nil {
		return 0
	}
	return n
}

func (d *decoder) count8(minElementSize int) int  { return d.count(int(d.u8()), minElementSize) }
func (d *decoder) count16(minElementSize int) int { return d.count(int(d.u16()), minElementSize) }

func (d *decoder) finish() error {
	if d.err == nil && len(d.buf) != 0 {
		d.fail("%d trailing bytes", len(d.buf))
	}
	return d.err
}

func (d *decoder) fail(format string, args ...any) {
	if d.err == nil {
		d.err = fmt.Errorf("%w: "+format, append([]any{ErrMalformed}, args...)...)
	}
}
