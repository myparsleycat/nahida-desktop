package tools

import (
	"regexp"
	"strconv"
	"strings"
)

const maxModelViewerDNFGroups = 128

var (
	modelViewerDNFClauseRE = regexp.MustCompile(`^\$(\w+)\s*(==|!=|<=|>=|<|>)\s*(-?[\w.]+)$`)
	modelViewerDNFBareRE   = regexp.MustCompile(`^\$(\w+)$`)
	modelViewerAliasRE     = regexp.MustCompile(`^\$(\w+)\s*=\s*(.+)$`)
)

func modelViewerDNFTrue() ModelViewerDNF  { return ModelViewerDNF{[]ModelViewerDNFClause{}} }
func modelViewerDNFFalse() ModelViewerDNF { return ModelViewerDNF{} }

func modelViewerDNFIsTrue(dnf ModelViewerDNF) bool {
	return len(dnf) == 1 && len(dnf[0]) == 0
}

func modelViewerDNFAnd(left, right ModelViewerDNF) ModelViewerDNF {
	if len(left) == 0 || len(right) == 0 {
		return modelViewerDNFFalse()
	}
	if modelViewerDNFIsTrue(left) {
		return cloneModelViewerDNF(right)
	}
	if modelViewerDNFIsTrue(right) {
		return cloneModelViewerDNF(left)
	}
	if len(left)*len(right) > maxModelViewerDNFGroups {
		return modelViewerDNFTrue()
	}
	var output ModelViewerDNF
	for _, leftGroup := range left {
		for _, rightGroup := range right {
			merged, possible := simplifyModelViewerDNFGroup(append(append([]ModelViewerDNFClause(nil), leftGroup...), rightGroup...))
			if possible && !containsModelViewerDNFGroup(output, merged) {
				output = append(output, merged)
			}
		}
	}
	return output
}

func modelViewerDNFOr(left, right ModelViewerDNF) ModelViewerDNF {
	if modelViewerDNFIsTrue(left) || modelViewerDNFIsTrue(right) {
		return modelViewerDNFTrue()
	}
	if len(left) == 0 {
		return cloneModelViewerDNF(right)
	}
	if len(right) == 0 {
		return cloneModelViewerDNF(left)
	}
	output := cloneModelViewerDNF(left)
	for _, group := range right {
		if !containsModelViewerDNFGroup(output, group) {
			output = append(output, append([]ModelViewerDNFClause(nil), group...))
		}
	}
	if len(output) > maxModelViewerDNFGroups {
		return modelViewerDNFTrue()
	}
	return output
}

func modelViewerDNFNot(input ModelViewerDNF) ModelViewerDNF {
	if len(input) == 0 {
		return modelViewerDNFTrue()
	}
	if modelViewerDNFIsTrue(input) {
		return modelViewerDNFFalse()
	}
	result := modelViewerDNFTrue()
	for _, group := range input {
		if len(group) == 0 {
			return modelViewerDNFFalse()
		}
		var alternatives ModelViewerDNF
		for _, clause := range group {
			clause.Negate = !clause.Negate
			alternatives = append(alternatives, []ModelViewerDNFClause{clause})
		}
		result = modelViewerDNFAnd(result, alternatives)
	}
	return result
}

func modelViewerConditionsToDNF(conditions []modelViewerConditionClause, variables map[string]any) ModelViewerDNF {
	result := modelViewerDNFTrue()
	aliases, _ := variables["__aliases"].(map[string]ModelViewerDNF)
	for _, condition := range conditions {
		parsed := parseModelViewerConditionDNF(condition.Expression, aliases, variables)
		if !condition.Expected {
			parsed = modelViewerDNFNot(parsed)
		}
		result = modelViewerDNFAnd(result, parsed)
		if len(result) == 0 {
			return result
		}
	}
	return result
}

type modelViewerDNFParser struct {
	tokens    []string
	position  int
	aliases   map[string]ModelViewerDNF
	variables map[string]any
}

func parseModelViewerConditionDNF(expression string, aliases map[string]ModelViewerDNF, variables map[string]any) ModelViewerDNF {
	parser := &modelViewerDNFParser{tokens: tokenizeModelViewerDNF(expression), aliases: aliases, variables: variables}
	return parser.parseOr()
}

func (p *modelViewerDNFParser) parseOr() ModelViewerDNF {
	node := p.parseAnd()
	for p.peek() == "||" {
		p.position++
		node = modelViewerDNFOr(node, p.parseAnd())
	}
	return node
}

func (p *modelViewerDNFParser) parseAnd() ModelViewerDNF {
	node := p.parseAtom()
	for p.peek() == "&&" {
		p.position++
		node = modelViewerDNFAnd(node, p.parseAtom())
	}
	return node
}

func (p *modelViewerDNFParser) parseAtom() ModelViewerDNF {
	token := p.peek()
	if token == "" {
		return modelViewerDNFTrue()
	}
	if token == "!" {
		p.position++
		return modelViewerDNFNot(p.parseAtom())
	}
	if token == "(" {
		p.position++
		node := p.parseOr()
		if p.peek() == ")" {
			p.position++
		}
		return node
	}
	if token == ")" || token == "&&" || token == "||" {
		p.position++
		return modelViewerDNFTrue()
	}
	p.position++
	return modelViewerDNFAtom(token, p.aliases, p.variables)
}

func (p *modelViewerDNFParser) peek() string {
	if p.position >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.position]
}

func tokenizeModelViewerDNF(expression string) []string {
	var tokens []string
	for position := 0; position < len(expression); {
		if expression[position] == ' ' || expression[position] == '\t' || expression[position] == '\r' || expression[position] == '\n' {
			position++
			continue
		}
		if position+1 < len(expression) && (expression[position:position+2] == "&&" || expression[position:position+2] == "||") {
			tokens = append(tokens, expression[position:position+2])
			position += 2
			continue
		}
		if expression[position] == '(' || expression[position] == ')' || expression[position] == '!' && (position+1 >= len(expression) || expression[position+1] != '=') {
			tokens = append(tokens, expression[position:position+1])
			position++
			continue
		}
		start := position
		for position < len(expression) {
			if expression[position] == '(' || expression[position] == ')' || expression[position] == '!' && (position+1 >= len(expression) || expression[position+1] != '=') || position+1 < len(expression) && (expression[position:position+2] == "&&" || expression[position:position+2] == "||") {
				break
			}
			position++
		}
		if token := strings.TrimSpace(expression[start:position]); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func modelViewerDNFAtom(raw string, aliases map[string]ModelViewerDNF, variables map[string]any) ModelViewerDNF {
	atom := strings.TrimSpace(raw)
	if atom == "" {
		return modelViewerDNFTrue()
	}
	if match := modelViewerDNFClauseRE.FindStringSubmatch(atom); match != nil {
		variable := modelViewerNormalizeKey(match[1])
		op, value := match[2], match[3]
		if op == "==" || op == "!=" {
			return ModelViewerDNF{{{Var: variable, Value: value, Negate: op == "!="}}}
		}
		return expandModelViewerDNFComparison(variable, op, value, variables)
	}
	if match := modelViewerDNFBareRE.FindStringSubmatch(atom); match != nil {
		if alias, exists := aliases[match[1]]; exists {
			return cloneModelViewerDNF(alias)
		}
		variable := modelViewerNormalizeKey(match[1])
		return ModelViewerDNF{{{Var: variable, Value: "0", Negate: true}}}
	}
	if value, err := strconv.ParseFloat(atom, 64); err == nil && value == 0 {
		return modelViewerDNFFalse()
	}
	return modelViewerDNFTrue()
}

func expandModelViewerDNFComparison(variable, operator, rawRight string, variables map[string]any) ModelViewerDNF {
	domain, _ := variables["__domain:"+variable].([]any)
	right, err := strconv.ParseFloat(rawRight, 64)
	if len(domain) == 0 || err != nil {
		return modelViewerDNFTrue()
	}
	var output ModelViewerDNF
	for _, value := range domain {
		left, numberErr := modelViewerNumber(value)
		if numberErr != nil {
			continue
		}
		matched := false
		switch operator {
		case "<":
			matched = left < right
		case "<=":
			matched = left <= right
		case ">":
			matched = left > right
		case ">=":
			matched = left >= right
		}
		if matched {
			output = append(output, []ModelViewerDNFClause{{Var: variable, Value: modelViewerString(value)}})
		}
	}
	if len(output) == len(domain) {
		return modelViewerDNFTrue()
	}
	return output
}

func buildModelViewerBoolAliases(sections []modINISection, variables map[string]any) map[string]ModelViewerDNF {
	rawDefinitions := make(map[string]string)
	for _, section := range sections {
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			match := modelViewerAliasRE.FindStringSubmatch(line)
			if match == nil || !strings.Contains(match[2], "==") && !strings.Contains(match[2], "!=") {
				continue
			}
			key := match[1]
			if _, exists := rawDefinitions[key]; !exists {
				rawDefinitions[key] = strings.TrimSpace(match[2])
			}
		}
	}
	aliases := make(map[string]ModelViewerDNF)
	for range 2 {
		for alias, expression := range rawDefinitions {
			dnf := parseModelViewerConditionDNF(expression, aliases, variables)
			if len(dnf) > 0 && !modelViewerDNFIsTrue(dnf) {
				aliases[alias] = dnf
			}
		}
	}
	return aliases
}

func simplifyModelViewerDNFGroup(group []ModelViewerDNFClause) ([]ModelViewerDNFClause, bool) {
	var output []ModelViewerDNFClause
	positive := make(map[string]string)
	negative := make(map[string]map[string]bool)
	for _, clause := range group {
		key := modelViewerNormalizeKey(clause.Var)
		clause.Var = key
		if clause.Negate {
			if positive[key] == clause.Value {
				return nil, false
			}
			if negative[key] == nil {
				negative[key] = make(map[string]bool)
			}
			negative[key][clause.Value] = true
		} else {
			if existing, exists := positive[key]; exists && existing != clause.Value || negative[key][clause.Value] {
				return nil, false
			}
			positive[key] = clause.Value
		}
		if !containsModelViewerDNFClause(output, clause) {
			output = append(output, clause)
		}
	}
	return output, true
}

func cloneModelViewerDNF(input ModelViewerDNF) ModelViewerDNF {
	output := make(ModelViewerDNF, len(input))
	for index, group := range input {
		output[index] = append([]ModelViewerDNFClause(nil), group...)
	}
	return output
}

func containsModelViewerDNFGroup(dnf ModelViewerDNF, wanted []ModelViewerDNFClause) bool {
	for _, group := range dnf {
		if len(group) != len(wanted) {
			continue
		}
		matched := true
		for index := range group {
			if group[index] != wanted[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func containsModelViewerDNFClause(group []ModelViewerDNFClause, wanted ModelViewerDNFClause) bool {
	for _, clause := range group {
		if clause == wanted {
			return true
		}
	}
	return false
}

func normalizeModelViewerDNFWithDomains(dnf ModelViewerDNF, variables map[string]any) ModelViewerDNF {
	if len(dnf) == 0 || modelViewerDNFIsTrue(dnf) {
		return dnf
	}
	var names []string
	for _, group := range dnf {
		for _, clause := range group {
			name := modelViewerNormalizeKey(clause.Var)
			found := false
			for _, existing := range names {
				if existing == name {
					found = true
					break
				}
			}
			if !found {
				names = append(names, name)
			}
		}
	}
	domains := make([][]any, len(names))
	combinations := 1
	for index, name := range names {
		domain, _ := variables["__domain:"+name].([]any)
		if len(domain) == 0 || combinations > 4096/len(domain) {
			return dnf
		}
		domains[index] = domain
		combinations *= len(domain)
	}
	state := make(map[string]string, len(names))
	var visit func(int) bool
	visit = func(index int) bool {
		if index == len(names) {
			return modelViewerDNFSatisfied(dnf, state)
		}
		for _, value := range domains[index] {
			state[names[index]] = modelViewerString(value)
			if !visit(index + 1) {
				return false
			}
		}
		return true
	}
	if visit(0) {
		return modelViewerDNFTrue()
	}
	return dnf
}

// normalizeModelViewerDNFWithTracked keeps only state that the viewer can
// actually control. Runtime guards such as WWMI's $mod_enabled and
// $object_detected are established by the game/plugin lifecycle; retaining
// them would leave every draw hidden at their declared bootstrap defaults.
func normalizeModelViewerDNFWithTracked(dnf ModelViewerDNF, tracked map[string]bool) ModelViewerDNF {
	if len(dnf) == 0 || modelViewerDNFIsTrue(dnf) {
		return dnf
	}
	output := make(ModelViewerDNF, 0, len(dnf))
	for _, group := range dnf {
		kept := make([]ModelViewerDNFClause, 0, len(group))
		for _, clause := range group {
			if !tracked[modelViewerNormalizeKey(clause.Var)] {
				continue
			}
			if !containsModelViewerDNFClause(kept, clause) {
				kept = append(kept, clause)
			}
		}
		if len(kept) == 0 {
			return modelViewerDNFTrue()
		}
		if !containsModelViewerDNFGroup(output, kept) {
			output = append(output, kept)
		}
	}
	return output
}

func modelViewerDNFSatisfied(dnf ModelViewerDNF, state map[string]string) bool {
	for _, group := range dnf {
		matched := true
		for _, clause := range group {
			equal := state[modelViewerNormalizeKey(clause.Var)] == clause.Value
			if equal == clause.Negate {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
