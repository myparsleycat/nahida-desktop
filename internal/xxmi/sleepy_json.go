package xxmi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

type sleepyJSONField struct {
	key   string
	value sleepyJSONValue
}

type sleepyJSONValue struct {
	kind   byte
	scalar any
	fields []sleepyJSONField
	items  []sleepyJSONValue
}

func parseSleepyJSON(data []byte) (sleepyJSONValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readSleepyJSONValue(decoder)
	if err != nil {
		return sleepyJSONValue{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return sleepyJSONValue{}, errors.New("unexpected data after Sleepy JSON")
	}
	return value, nil
}

func readSleepyJSONValue(decoder *json.Decoder) (sleepyJSONValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return sleepyJSONValue{}, err
	}
	switch token {
	case json.Delim('{'):
		value := sleepyJSONValue{kind: '{'}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return sleepyJSONValue{}, err
			}
			name, ok := key.(string)
			if !ok {
				return sleepyJSONValue{}, errors.New("invalid Sleepy JSON object key")
			}
			child, err := readSleepyJSONValue(decoder)
			if err != nil {
				return sleepyJSONValue{}, err
			}
			value.fields = append(value.fields, sleepyJSONField{key: name, value: child})
		}
		_, err := decoder.Token()
		return value, err
	case json.Delim('['):
		value := sleepyJSONValue{kind: '['}
		for decoder.More() {
			child, err := readSleepyJSONValue(decoder)
			if err != nil {
				return sleepyJSONValue{}, err
			}
			value.items = append(value.items, child)
		}
		_, err := decoder.Token()
		return value, err
	default:
		return sleepyJSONValue{scalar: token}, nil
	}
}

func (value *sleepyJSONValue) field(key string) *sleepyJSONValue {
	for index := range value.fields {
		if value.fields[index].key == key {
			return &value.fields[index].value
		}
	}
	return nil
}

func (value *sleepyJSONValue) setField(key string, child sleepyJSONValue) {
	for index := range value.fields {
		if value.fields[index].key == key {
			value.fields[index].value = child
			return
		}
	}
	value.fields = append(value.fields, sleepyJSONField{key: key, value: child})
}

func serializeSleepyJSON(value sleepyJSONValue) ([]byte, error) {
	var output strings.Builder
	output.WriteString("\r\n")
	if err := writeSleepyJSON(&output, value, 0); err != nil {
		return nil, err
	}
	return []byte(output.String()), nil
}

func writeSleepyJSON(output *strings.Builder, value sleepyJSONValue, level int) error {
	if level > 64 {
		return errors.New("sleepy JSON nesting exceeds limit")
	}
	switch value.kind {
	case '{':
		output.WriteString("{\r\n")
		width := 0
		for index, field := range value.fields {
			width = max(width, len(field.key))
			output.WriteString(strings.Repeat(" ", (level+1)*4))
			output.WriteByte('"')
			output.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(field.key))
			output.WriteByte('"')
			output.WriteString(strings.Repeat(" ", width-len(field.key)+1))
			output.WriteString(": ")
			if err := writeSleepyJSON(output, field.value, level+1); err != nil {
				return err
			}
			if index < len(value.fields)-1 {
				output.WriteByte(',')
			}
			output.WriteString("\r\n")
		}
		output.WriteString(strings.Repeat(" ", level*4))
		output.WriteByte('}')
	case '[':
		output.WriteString("[\r\n")
		for index, item := range value.items {
			output.WriteString(strings.Repeat(" ", (level+1)*4))
			if err := writeSleepyJSON(output, item, level+1); err != nil {
				return err
			}
			if index < len(value.items)-1 {
				output.WriteByte(',')
			}
			output.WriteString("\r\n")
		}
		output.WriteString(strings.Repeat(" ", level*4))
		output.WriteByte(']')
	default:
		if text, ok := value.scalar.(string); ok {
			output.WriteByte('"')
			output.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text))
			output.WriteByte('"')
			return nil
		}
		data, err := json.Marshal(value.scalar)
		if err != nil {
			return fmt.Errorf("serialize Sleepy JSON value: %w", err)
		}
		output.Write(data)
	}
	return nil
}

// compactSleepyJSON writes value like Python's json.dumps(value, separators=(",", ":")) with the default
// ensure_ascii, keeping the original key order and number text.
func compactSleepyJSON(value sleepyJSONValue) ([]byte, error) {
	var output strings.Builder
	if err := writeCompactJSON(&output, value, 0); err != nil {
		return nil, err
	}
	return []byte(output.String()), nil
}

func writeCompactJSON(output *strings.Builder, value sleepyJSONValue, level int) error {
	if level > 64 {
		return errors.New("JSON nesting exceeds limit")
	}
	switch value.kind {
	case '{':
		output.WriteByte('{')
		for index, field := range value.fields {
			if index > 0 {
				output.WriteByte(',')
			}
			writeASCIIJSONString(output, field.key)
			output.WriteByte(':')
			if err := writeCompactJSON(output, field.value, level+1); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case '[':
		output.WriteByte('[')
		for index, item := range value.items {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCompactJSON(output, item, level+1); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	default:
		if text, ok := value.scalar.(string); ok {
			writeASCIIJSONString(output, text)
			return nil
		}
		data, err := json.Marshal(value.scalar)
		if err != nil {
			return fmt.Errorf("serialize JSON value: %w", err)
		}
		output.Write(data)
	}
	return nil
}

// writeASCIIJSONString escapes like Python's ensure_ascii: every character outside printable ASCII becomes
// a lowercase \uXXXX escape, using a surrogate pair above the Basic Multilingual Plane.
func writeASCIIJSONString(output *strings.Builder, text string) {
	output.WriteByte('"')
	for _, character := range text {
		switch character {
		case '"':
			output.WriteString(`\"`)
		case '\\':
			output.WriteString(`\\`)
		case '\b':
			output.WriteString(`\b`)
		case '\f':
			output.WriteString(`\f`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			if character >= 0x20 && character < 0x7f {
				output.WriteRune(character)
				continue
			}
			if character > 0xffff {
				high, low := utf16.EncodeRune(character)
				fmt.Fprintf(output, `\u%04x\u%04x`, high, low)
				continue
			}
			fmt.Fprintf(output, `\u%04x`, character)
		}
	}
	output.WriteByte('"')
}
