package gameplatform

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// vdfNode is one key of a Valve KeyValues text document. A node holds either a string or a block of
// child nodes. Offsets point into the parsed bytes so a single value can be rewritten without
// reformatting the rest of a file that Steam owns.
type vdfNode struct {
	key      string
	value    string
	children []*vdfNode
	block    bool
	depth    int

	// valueStart and valueEnd span the value token, quotes included, of a string node.
	valueStart, valueEnd int
	// bodyStart is the offset right after the opening brace of a block node.
	bodyStart int
}

// child returns the first child named key. Steam treats key names as case-insensitive.
func (n *vdfNode) child(key string) *vdfNode {
	if n == nil {
		return nil
	}
	for _, child := range n.children {
		if strings.EqualFold(child.key, key) {
			return child
		}
	}
	return nil
}

// entries returns the children of a block and nothing for a missing node.
func (n *vdfNode) entries() []*vdfNode {
	if n == nil {
		return nil
	}
	return n.children
}

// find walks nested blocks and returns nil when any key on the way is missing.
func (n *vdfNode) find(keys ...string) *vdfNode {
	node := n
	for _, key := range keys {
		node = node.child(key)
	}
	return node
}

// text returns the string value of the child named key.
func (n *vdfNode) text(key string) (string, bool) {
	child := n.child(key)
	if child == nil || child.block {
		return "", false
	}
	return child.value, true
}

type vdfParser struct {
	data []byte
	pos  int
}

// parseVDF reads a KeyValues text document into a root block whose children are the top-level keys.
func parseVDF(data []byte) (*vdfNode, error) {
	parser := &vdfParser{data: data}
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		parser.pos = 3
	}
	root := &vdfNode{block: true, depth: -1}
	if err := parser.parseBlock(root, false); err != nil {
		return nil, err
	}
	return root, nil
}

func (p *vdfParser) parseBlock(parent *vdfNode, nested bool) error {
	for {
		p.skipSpace()
		if p.pos >= len(p.data) {
			if nested {
				return errors.New("vdf: block is not closed")
			}
			return nil
		}
		if p.data[p.pos] == '}' {
			if !nested {
				return fmt.Errorf("vdf: unexpected } at offset %d", p.pos)
			}
			p.pos++
			return nil
		}
		key, _, _, err := p.token()
		if err != nil {
			return err
		}

		// Conditionals such as [$WIN32] follow a value and do not carry data.
		if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
			continue
		}
		node := &vdfNode{key: key, depth: parent.depth + 1}
		p.skipSpace()
		if p.pos >= len(p.data) {
			return fmt.Errorf("vdf: key %q has no value", key)
		}
		if p.data[p.pos] == '{' {
			p.pos++
			node.block, node.bodyStart = true, p.pos
			if err := p.parseBlock(node, true); err != nil {
				return err
			}
		} else {
			node.value, node.valueStart, node.valueEnd, err = p.token()
			if err != nil {
				return err
			}
		}
		parent.children = append(parent.children, node)
	}
}

func (p *vdfParser) skipSpace() {
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.pos++
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/':
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// token reads one quoted or bare token and returns its unescaped text with its byte span.
func (p *vdfParser) token() (text string, start, end int, err error) {
	start = p.pos
	if p.data[p.pos] != '"' {
		for p.pos < len(p.data) && !strings.ContainsRune(" \t\r\n{}\"", rune(p.data[p.pos])) {
			p.pos++
		}
		if p.pos == start {
			return "", 0, 0, fmt.Errorf("vdf: unexpected %q at offset %d", p.data[p.pos], p.pos)
		}
		return string(p.data[start:p.pos]), start, p.pos, nil
	}
	p.pos++
	var out strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		p.pos++
		switch {
		case c == '"':
			return out.String(), start, p.pos, nil
		case c == '\\' && p.pos < len(p.data):
			escaped := p.data[p.pos]
			p.pos++
			switch escaped {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case '\\', '"':
				out.WriteByte(escaped)
			default:
				out.WriteByte('\\')
				out.WriteByte(escaped)
			}
		default:
			out.WriteByte(c)
		}
	}
	return "", 0, 0, fmt.Errorf("vdf: string starting at offset %d is not closed", start)
}

func quoteVDF(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(value) + `"`
}

// setVDFString returns data with the string key of block set to value. An existing value is replaced
// in place; a missing key becomes the first entry of the block. Every other byte is kept.
func setVDFString(data []byte, block *vdfNode, key, value string) ([]byte, error) {
	if block == nil || !block.block {
		return nil, errors.New("vdf: target is not a block")
	}
	if existing := block.child(key); existing != nil {
		if existing.block {
			return nil, fmt.Errorf("vdf: %q is a block", key)
		}
		return bytes.Join(
			[][]byte{data[:existing.valueStart], []byte(quoteVDF(value)), data[existing.valueEnd:]},
			nil,
		), nil
	}

	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	entry := newline + strings.Repeat("\t", block.depth+1) + quoteVDF(key) + "\t\t" + quoteVDF(value)
	return bytes.Join([][]byte{data[:block.bodyStart], []byte(entry), data[block.bodyStart:]}, nil), nil
}
