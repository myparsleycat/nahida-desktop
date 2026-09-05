package zzmi

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"nahida.live/desktop/internal/infra"
)

const maxLiteralDepth = 64

type identifier string

type literalParser struct {
	source string
	pos    int
	depth  int
}

func ParseCharacterModule(source []byte) (map[string][]Command, error) {
	text := string(source)
	function := strings.Index(text, "def get_hash_commands")
	if function < 0 {
		return nil, errors.New("missing get_hash_commands")
	}
	returnAt := strings.Index(text[function:], "return")
	if returnAt < 0 {
		return nil, errors.New("get_hash_commands has no return")
	}
	p := literalParser{source: text, pos: function + returnAt + len("return")}
	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("get_hash_commands must return a dictionary")
	}
	commands := make(map[string][]Command, len(raw))
	for hash, item := range raw {
		if !isHash(hash) {
			return nil, fmt.Errorf("invalid hash key %q", hash)
		}
		sequence, ok := item.([]any)
		if !ok {
			return nil, fmt.Errorf("commands for %s are not a sequence", hash)
		}
		compiled := make([]Command, 0, len(sequence))
		for _, rawCommand := range sequence {
			command, compileErr := compileCommand(rawCommand)
			if compileErr != nil {
				return nil, fmt.Errorf("compile %s: %w", hash, compileErr)
			}
			compiled = append(compiled, command)
		}
		commands[strings.ToLower(hash)] = compiled
	}
	return commands, nil
}

func ParseRemapper(source []byte, names ...string) (map[string]any, error) {
	text := string(source)
	result := make(map[string]any, len(names))
	for _, name := range names {
		start := findAssignment(text, name)
		if start < 0 {
			return nil, fmt.Errorf("missing remapper constant %s", name)
		}
		p := literalParser{source: text, pos: start}
		value, err := p.parseValue()
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		if err := rejectIdentifiers(value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		result[name] = value
	}
	return result, nil
}

func findAssignment(source, name string) int {
	for offset := 0; offset < len(source); {
		lineEnd := strings.IndexByte(source[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(source) - offset
		}
		line := source[offset : offset+lineEnd]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, name) {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, name))
			if strings.HasPrefix(rest, "=") {
				column := strings.Index(line, "=")
				return offset + column + 1
			}
		}
		offset += lineEnd + 1
	}
	return -1
}

func compileCommand(value any) (Command, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 2 {
		return Command{}, errors.New("command must be a one- or two-item tuple")
	}
	op, ok := items[0].(identifier)
	if !ok {
		return Command{}, errors.New("command operation is not an identifier")
	}
	if _, ok := allowedOperations[string(op)]; !ok {
		return Command{}, fmt.Errorf("operation %q is not allowed", op)
	}
	command := Command{Op: string(op)}
	if len(items) == 1 {
		return command, nil
	}
	if kwargs, ok := items[1].(map[string]any); ok {
		if err := rejectIdentifiers(kwargs); err != nil {
			return Command{}, err
		}
		command.Kwargs = kwargs
		return command, nil
	}
	args, ok := items[1].([]any)
	if !ok {
		args = []any{items[1]}
	}
	if err := rejectIdentifiers(args); err != nil {
		return Command{}, err
	}
	command.Args = args
	return command, nil
}

func rejectIdentifiers(value any) error {
	switch item := value.(type) {
	case identifier:
		return fmt.Errorf("unexpected identifier %q", item)
	case []any:
		for _, child := range item {
			if err := rejectIdentifiers(child); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, child := range item {
			if err := rejectIdentifiers(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *literalParser) parseValue() (any, error) {
	p.skipSpace()
	if p.depth >= maxLiteralDepth || p.pos >= len(p.source) {
		return nil, errors.New("literal is too deeply nested or incomplete")
	}
	p.depth++
	defer func() { p.depth-- }()
	switch p.source[p.pos] {
	case '\'', '"':
		return p.parseAdjacentStrings()
	case '{':
		return p.parseBrace()
	case '[', '(':
		return p.parseSequence(p.source[p.pos])
	default:
		if p.source[p.pos] == '-' || isDigit(p.source[p.pos]) {
			return p.parseInteger()
		}
		if isIdentifierStart(p.source[p.pos]) {
			return p.parseIdentifier()
		}
	}
	return nil, fmt.Errorf("unsupported Python literal at byte %d", p.pos)
}

func (p *literalParser) parseSequence(open byte) ([]any, error) {
	close := byte(']')
	if open == '(' {
		close = ')'
	}
	p.pos++
	items := []any{}
	for {
		p.skipSpace()
		if p.pos >= len(p.source) {
			return nil, errors.New("unterminated sequence")
		}
		if p.source[p.pos] == close {
			p.pos++
			return items, nil
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		items = append(items, value)
		p.skipSpace()
		if p.pos < len(p.source) && p.source[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.pos >= len(p.source) || p.source[p.pos] != close {
			return nil, fmt.Errorf("expected %q at byte %d", close, p.pos)
		}
	}
}

func (p *literalParser) parseBrace() (any, error) {
	p.pos++
	p.skipSpace()
	if p.pos < len(p.source) && p.source[p.pos] == '}' {
		p.pos++
		return map[string]any{}, nil
	}
	first, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos < len(p.source) && p.source[p.pos] == ':' {
		result := map[string]any{}
		for {
			p.pos++
			value, valueErr := p.parseValue()
			if valueErr != nil {
				return nil, valueErr
			}
			key, keyErr := literalKey(first)
			if keyErr != nil {
				return nil, keyErr
			}
			result[key] = value
			p.skipSpace()
			if p.pos < len(p.source) && p.source[p.pos] == ',' {
				p.pos++
				p.skipSpace()
				if p.pos < len(p.source) && p.source[p.pos] == '}' {
					p.pos++
					return result, nil
				}
				first, err = p.parseValue()
				if err != nil {
					return nil, err
				}
				p.skipSpace()
				if p.pos >= len(p.source) || p.source[p.pos] != ':' {
					return nil, errors.New("mixed set and dictionary literal")
				}
				continue
			}
			if p.pos < len(p.source) && p.source[p.pos] == '}' {
				p.pos++
				return result, nil
			}
			return nil, errors.New("unterminated dictionary")
		}
	}
	items := []any{first}
	for {
		p.skipSpace()
		if p.pos < len(p.source) && p.source[p.pos] == '}' {
			p.pos++
			return items, nil
		}
		if p.pos >= len(p.source) || p.source[p.pos] != ',' {
			return nil, errors.New("unterminated set")
		}
		p.pos++
		p.skipSpace()
		if p.pos < len(p.source) && p.source[p.pos] == '}' {
			p.pos++
			return items, nil
		}
		value, valueErr := p.parseValue()
		if valueErr != nil {
			return nil, valueErr
		}
		items = append(items, value)
	}
}

func literalKey(value any) (string, error) {
	switch key := value.(type) {
	case string:
		return key, nil
	case int64:
		return strconv.FormatInt(key, 10), nil
	default:
		return "", fmt.Errorf("unsupported dictionary key %T", value)
	}
}

func (p *literalParser) parseAdjacentStrings() (string, error) {
	var result strings.Builder
	for {
		value, err := p.parseString()
		if err != nil {
			return "", err
		}
		result.WriteString(value)
		p.skipSpace()
		if p.pos >= len(p.source) || (p.source[p.pos] != '\'' && p.source[p.pos] != '"') {
			return result.String(), nil
		}
	}
}

func (p *literalParser) parseString() (string, error) {
	quote := p.source[p.pos]
	triple := p.pos+2 < len(p.source) && p.source[p.pos+1] == quote && p.source[p.pos+2] == quote
	if triple {
		p.pos += 3
	} else {
		p.pos++
	}
	var result strings.Builder
	for p.pos < len(p.source) {
		if triple && p.pos+2 < len(p.source) && p.source[p.pos] == quote && p.source[p.pos+1] == quote && p.source[p.pos+2] == quote {
			p.pos += 3
			return result.String(), nil
		}
		if !triple && p.source[p.pos] == quote {
			p.pos++
			return result.String(), nil
		}
		char := p.source[p.pos]
		p.pos++
		if char != '\\' {
			result.WriteByte(char)
			continue
		}
		if p.pos >= len(p.source) {
			return "", errors.New("unterminated string escape")
		}
		escape := p.source[p.pos]
		p.pos++
		switch escape {
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 't':
			result.WriteByte('\t')
		case '\\', '\'', '"':
			result.WriteByte(escape)
		case '\n':
		case 'x', 'u', 'U':
			digits := 2
			if escape == 'u' {
				digits = 4
			}
			if escape == 'U' {
				digits = 8
			}
			if p.pos+digits > len(p.source) {
				return "", errors.New("incomplete Unicode escape")
			}
			value, err := strconv.ParseUint(p.source[p.pos:p.pos+digits], 16, 32)
			if err != nil || !utf8.ValidRune(rune(value)) {
				return "", infra.WithCause(errors.New("invalid Unicode escape"), err)
			}
			result.WriteRune(rune(value))
			p.pos += digits
		default:
			result.WriteByte('\\')
			result.WriteByte(escape)
		}
	}
	return "", errors.New("unterminated string")
}

func (p *literalParser) parseInteger() (int64, error) {
	start := p.pos
	if p.source[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.source) && isDigit(p.source[p.pos]) {
		p.pos++
	}
	return strconv.ParseInt(p.source[start:p.pos], 10, 64)
}

func (p *literalParser) parseIdentifier() (any, error) {
	start := p.pos
	for p.pos < len(p.source) && isIdentifierPart(p.source[p.pos]) {
		p.pos++
	}
	name := p.source[start:p.pos]
	switch name {
	case "True":
		return true, nil
	case "False":
		return false, nil
	case "None":
		return nil, nil
	default:
		return identifier(name), nil
	}
}

func (p *literalParser) skipSpace() {
	for p.pos < len(p.source) {
		switch p.source[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		case '#':
			for p.pos < len(p.source) && p.source[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func isDigit(char byte) bool { return char >= '0' && char <= '9' }
func isIdentifierStart(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}
func isIdentifierPart(char byte) bool { return isIdentifierStart(char) || isDigit(char) }
