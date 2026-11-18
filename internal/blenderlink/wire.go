package blenderlink

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

func (e *encoder) str(v string) {
	if len(v) > math.MaxUint16 || !utf8.ValidString(v) {
		e.fail("string of %d bytes", len(v))
		return
	}
	e.u16(uint16(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *encoder) bytes32(v []byte) {
	if uint64(len(v)) > math.MaxUint32 {
		e.fail("%d bytes", len(v))
		return
	}
	e.u32(uint32(len(v)))
	e.buf = append(e.buf, v...)
}

func (e *encoder) raw(v []byte) { e.buf = append(e.buf, v...) }

func (e *encoder) count16(n int) bool {
	if n > math.MaxUint16 {
		e.fail("%d elements", n)
		return false
	}
	e.u16(uint16(n))
	return true
}

func (e *encoder) strs(values []string) {
	if e.count16(len(values)) {
		for _, v := range values {
			e.str(v)
		}
	}
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
	if n < 0 || len(d.buf) < n {
		d.fail("payload ends early")
		return nil
	}
	v := d.buf[:n:n]
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

// bytes32 returns a slice of the payload, not a copy.
func (d *decoder) bytes32() []byte {
	n := d.u32()
	if d.err == nil && uint64(n) > uint64(len(d.buf)) {
		d.fail("%d bytes in %d", n, len(d.buf))
	}
	v := d.take(int(n))
	if v == nil {
		return []byte{}
	}
	return v
}

// count16 reads an element count and bounds it by the bytes left, so a corrupt
// count cannot make the caller allocate far more than the frame holds.
func (d *decoder) count16(minElementSize int) int {
	n := int(d.u16())
	if d.err == nil && n*minElementSize > len(d.buf) {
		d.fail("%d elements in %d bytes", n, len(d.buf))
	}
	if d.err != nil {
		return 0
	}
	return n
}

func (d *decoder) strs() []string {
	values := make([]string, d.count16(2))
	for i := range values {
		values[i] = d.str()
	}
	return values
}

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
