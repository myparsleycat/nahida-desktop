package efmioutline

import (
	"regexp"
	"strconv"
	"strings"
)

// tri is three-valued (Kleene) logic: conditions on mod variables are unknown
// to the analyzer, while conditions on vs are decided for a given pass.
type tri uint8

const (
	triFalse tri = iota
	triUnknown
	triTrue
)

func (t tri) not() tri {
	switch t {
	case triTrue:
		return triFalse
	case triFalse:
		return triTrue
	default:
		return triUnknown
	}
}

func triAnd(left, right tri) tri {
	if left == triFalse || right == triFalse {
		return triFalse
	}
	if left == triTrue && right == triTrue {
		return triTrue
	}
	return triUnknown
}

func triOr(left, right tri) tri {
	if left == triTrue || right == triTrue {
		return triTrue
	}
	if left == triFalse && right == triFalse {
		return triFalse
	}
	return triUnknown
}

var (
	vsWordRE = regexp.MustCompile(`(?i)(?:^|[^\w$\\.-])vs(?:[^\w\\-]|$)`)
	vsAtomRE = regexp.MustCompile(`(?i)^vs\s*(===|==|!==|!=)\s*(\d+)(?:\.0+)?$`)
)

type node interface {
	eval(pass int, ignoreExclusion bool) tri
}

type andNode struct{ left, right node }

func (n andNode) eval(pass int, ignore bool) tri {
	return triAnd(n.left.eval(pass, ignore), n.right.eval(pass, ignore))
}

type orNode struct{ left, right node }

func (n orNode) eval(pass int, ignore bool) tri {
	return triOr(n.left.eval(pass, ignore), n.right.eval(pass, ignore))
}

type notNode struct{ inner node }

func (n notNode) eval(pass int, ignore bool) tri { return n.inner.eval(pass, ignore).not() }

type vsAtom struct {
	equal bool
	value int
}

// eval treats "vs != 203" as true when ignoreExclusion is set, which recovers
// the condition as it was before this tool excluded a draw from the outline pass.
func (a vsAtom) eval(pass int, ignore bool) tri {
	if ignore && !a.equal && a.value == outlinePass {
		return triTrue
	}
	if (pass == a.value) == a.equal {
		return triTrue
	}
	return triFalse
}

type opaqueAtom struct{}

func (opaqueAtom) eval(int, bool) tri { return triUnknown }

type condition struct {
	text string
	root node
	// vs reports whether the text mentions vs at all; vsUnknown whether it does
	// so in a form the analyzer cannot evaluate.
	vs        bool
	vsUnknown bool
	vsAtoms   []vsAtom
}

func (c *condition) eval(pass int, ignore bool) tri {
	if c == nil {
		return triTrue
	}
	if c.root == nil {
		return triUnknown
	}
	return c.root.eval(pass, ignore)
}

func (c *condition) isOr() bool {
	_, ok := c.root.(orNode)
	return ok
}

func parseCondition(text string) *condition {
	c := &condition{text: strings.TrimSpace(text)}
	c.vs = vsWordRE.MatchString(c.text)
	tokens, ok := tokenizeCondition(c.text)
	if ok {
		p := conditionParser{tokens: tokens, cond: c}
		c.root, ok = p.parseOr()
		ok = ok && p.pos == len(tokens)
	}
	if !ok {
		c.root = nil
		c.vsAtoms = nil
		c.vsUnknown = c.vs
	}
	return c
}

type tokenKind uint8

const (
	tokenAtom tokenKind = iota
	tokenAnd
	tokenOr
	tokenNot
	tokenOpen
	tokenClose
)

type token struct {
	kind tokenKind
	text string
}

func tokenizeCondition(text string) ([]token, bool) {
	var tokens []token
	var atom strings.Builder
	flush := func() {
		if value := strings.TrimSpace(atom.String()); value != "" {
			tokens = append(tokens, token{kind: tokenAtom, text: value})
		}
		atom.Reset()
	}

	for i := 0; i < len(text); i++ {
		switch {
		case strings.HasPrefix(text[i:], "&&"):
			flush()
			tokens = append(tokens, token{kind: tokenAnd})
			i++
		case strings.HasPrefix(text[i:], "||"):
			flush()
			tokens = append(tokens, token{kind: tokenOr})
			i++
		case text[i] == '(':
			flush()
			tokens = append(tokens, token{kind: tokenOpen})
		case text[i] == ')':
			flush()
			tokens = append(tokens, token{kind: tokenClose})
		case text[i] == '!' && strings.TrimSpace(atom.String()) == "" &&
			strings.HasPrefix(strings.TrimLeft(text[i+1:], " \t"), "("):
			tokens = append(tokens, token{kind: tokenNot})
		default:
			atom.WriteByte(text[i])
		}
	}
	flush()
	return tokens, len(tokens) > 0
}

type conditionParser struct {
	tokens []token
	pos    int
	cond   *condition
}

func (p *conditionParser) peek() (token, bool) {
	if p.pos >= len(p.tokens) {
		return token{}, false
	}
	return p.tokens[p.pos], true
}

func (p *conditionParser) parseOr() (node, bool) {
	left, ok := p.parseAnd()
	for ok {
		next, more := p.peek()
		if !more || next.kind != tokenOr {
			break
		}
		p.pos++
		var right node
		right, ok = p.parseAnd()
		left = orNode{left: left, right: right}
	}
	return left, ok
}

func (p *conditionParser) parseAnd() (node, bool) {
	left, ok := p.parseUnary()
	for ok {
		next, more := p.peek()
		if !more || next.kind != tokenAnd {
			break
		}
		p.pos++
		var right node
		right, ok = p.parseUnary()
		left = andNode{left: left, right: right}
	}
	return left, ok
}

func (p *conditionParser) parseUnary() (node, bool) {
	next, ok := p.peek()
	if !ok {
		return nil, false
	}
	p.pos++

	switch next.kind {
	case tokenNot:
		inner, ok := p.parseUnary()
		return notNode{inner: inner}, ok
	case tokenOpen:
		inner, ok := p.parseOr()
		if closing, more := p.peek(); !ok || !more || closing.kind != tokenClose {
			return nil, false
		}
		p.pos++
		return inner, true
	case tokenAtom:
		return p.atom(next.text), true
	default:
		return nil, false
	}
}

func (p *conditionParser) atom(text string) node {
	if match := vsAtomRE.FindStringSubmatch(text); match != nil {
		value, err := strconv.Atoi(match[2])
		if err == nil {
			atom := vsAtom{equal: !strings.HasPrefix(match[1], "!"), value: value}
			p.cond.vsAtoms = append(p.cond.vsAtoms, atom)
			return atom
		}
	}
	if vsWordRE.MatchString(text) {
		p.cond.vsUnknown = true
	}
	return opaqueAtom{}
}
