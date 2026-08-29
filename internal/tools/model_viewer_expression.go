package tools

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type modelViewerExpressionToken struct {
	kind  string
	text  string
	value any
}

type modelViewerExpressionParser struct {
	tokens    []modelViewerExpressionToken
	index     int
	variables map[string]any
}

var modelViewerDrawVariableRE = regexp.MustCompile(`^\$(\w+)$`)

// parseModelViewerDrawIndexed maps 3DMigoto draw opcodes onto the viewer's
// IndexCount/StartIndex/BaseVertex triple. drawindexedinstanced keeps the D3D
// argument order (IndexCount, InstanceCount, StartIndex, BaseVertex, StartInstance)
// and drops the instance arguments, which are not used for static preview.
func parseModelViewerDrawIndexed(key, value string, variables map[string]any) (modelViewerDrawInstruction, bool) {
	if strings.EqualFold(strings.TrimSpace(value), "auto") {
		return modelViewerDrawInstruction{Auto: true}, true
	}
	parts := strings.Split(value, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	var countToken, startToken, baseToken string
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "drawindexed":
		if len(parts) != 3 {
			return modelViewerDrawInstruction{}, false
		}
		countToken, startToken, baseToken = parts[0], parts[1], parts[2]
	case "drawindexedinstanced":
		if len(parts) != 5 {
			return modelViewerDrawInstruction{}, false
		}
		countToken, startToken, baseToken = parts[0], parts[2], parts[3]
	default:
		return modelViewerDrawInstruction{}, false
	}
	count, countOK := resolveModelViewerDrawNumber(countToken, variables)
	start, startOK := resolveModelViewerDrawNumber(startToken, variables)
	base, baseOK := resolveModelViewerDrawNumber(baseToken, variables)
	if !countOK || !startOK || !baseOK {
		return modelViewerDrawInstruction{}, false
	}
	return modelViewerDrawInstruction{IndexCount: count, StartIndex: start, BaseVertex: base}, true
}

// resolveModelViewerDrawNumber intentionally accepts only the forms supported
// by Electron's resolveDrawNumber: an integer literal or one bare variable
// whose default is numeric. General expression evaluation is not used here.
func resolveModelViewerDrawNumber(token string, variables map[string]any) (int, bool) {
	token = strings.TrimSpace(token)
	if value, err := strconv.Atoi(token); err == nil {
		return value, value >= 0
	}
	match := modelViewerDrawVariableRE.FindStringSubmatch(token)
	if match == nil {
		return 0, false
	}
	value, exists := variables[modelViewerNormalizeKey(match[1])]
	if !exists {
		return 0, false
	}
	number, err := modelViewerNumber(value)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number != math.Trunc(number) {
		return 0, false
	}
	return int(number), true
}

func evaluateModelViewerCondition(expression string, variables map[string]any) bool {
	value, err := evaluateModelViewerExpression(expression, variables)
	return err == nil && modelViewerTruthy(value)
}

func evaluateModelViewerNumeric(expression string, variables map[string]any) (int, bool) {
	value, err := evaluateModelViewerExpression(expression, variables)
	if err != nil {
		return 0, false
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return int(number), true
}

func evaluateModelViewerExpression(expression string, variables map[string]any) (any, error) {
	tokens, err := tokenizeModelViewerExpression(expression)
	if err != nil {
		return nil, err
	}
	parser := &modelViewerExpressionParser{tokens: tokens, variables: variables}
	value, err := parser.logicalOr()
	if err != nil {
		return nil, err
	}
	if parser.index != len(tokens) {
		return nil, fmt.Errorf("unexpected token in expression: %s", expression)
	}
	return value, nil
}

func tokenizeModelViewerExpression(expression string) ([]modelViewerExpressionToken, error) {
	var tokens []modelViewerExpressionToken
	for index := 0; index < len(expression); {
		char := rune(expression[index])
		if unicode.IsSpace(char) {
			index++
			continue
		}
		if char >= '0' && char <= '9' {
			end := index + 1
			for end < len(expression) && expression[end] >= '0' && expression[end] <= '9' {
				end++
			}
			if end < len(expression) && expression[end] == '.' {
				end++
				for end < len(expression) && expression[end] >= '0' && expression[end] <= '9' {
					end++
				}
			}
			value, err := strconv.ParseFloat(expression[index:end], 64)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, modelViewerExpressionToken{kind: "number", value: value})
			index = end
			continue
		}
		if char == '\'' || char == '"' {
			quote := byte(char)
			end := index + 1
			var builder strings.Builder
			closed := false
			for end < len(expression) {
				if expression[end] == '\\' && end+1 < len(expression) {
					builder.WriteByte(expression[end+1])
					end += 2
					continue
				}
				if expression[end] == quote {
					closed = true
					end++
					break
				}
				builder.WriteByte(expression[end])
				end++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string literal in expression: %s", expression)
			}
			tokens = append(tokens, modelViewerExpressionToken{kind: "string", value: builder.String()})
			index = end
			continue
		}
		matched := false
		for _, operator := range []string{"===", "!==", "&&", "||", "==", "!=", "<=", ">=", "//", "+", "-", "*", "/", "%", "!", "<", ">", "="} {
			if strings.HasPrefix(expression[index:], operator) {
				tokens = append(tokens, modelViewerExpressionToken{kind: "operator", text: operator})
				index += len(operator)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if char == '(' || char == ')' {
			tokens = append(tokens, modelViewerExpressionToken{kind: "paren", text: string(char)})
			index++
			continue
		}
		if char == '$' || char == '_' || char == '\\' || unicode.IsLetter(char) {
			end := index + 1
			for end < len(expression) {
				value := rune(expression[end])
				if value == '_' || value == '.' || value == '\\' || unicode.IsLetter(value) || unicode.IsDigit(value) {
					end++
					continue
				}
				break
			}
			value := expression[index:end]
			switch strings.ToLower(value) {
			case "true":
				tokens = append(tokens, modelViewerExpressionToken{kind: "boolean", value: true})
			case "false":
				tokens = append(tokens, modelViewerExpressionToken{kind: "boolean", value: false})
			default:
				tokens = append(tokens, modelViewerExpressionToken{kind: "identifier", text: value})
			}
			index = end
			continue
		}
		return nil, fmt.Errorf("unsupported token %q in expression: %s", char, expression)
	}
	return tokens, nil
}

func (p *modelViewerExpressionParser) logicalOr() (any, error) {
	left, err := p.logicalAnd()
	for err == nil && p.match("||") {
		var right any
		right, err = p.logicalAnd()
		left = modelViewerTruthy(left) || modelViewerTruthy(right)
	}
	return left, err
}

func (p *modelViewerExpressionParser) logicalAnd() (any, error) {
	left, err := p.equality()
	for err == nil && p.match("&&") {
		var right any
		right, err = p.equality()
		left = modelViewerTruthy(left) && modelViewerTruthy(right)
	}
	return left, err
}

func (p *modelViewerExpressionParser) equality() (any, error) {
	left, err := p.comparison()
	for err == nil {
		if p.match("===") || p.match("==") || p.match("=") {
			var right any
			right, err = p.comparison()
			left = modelViewerEqual(left, right)
			continue
		}
		if p.match("!==") || p.match("!=") {
			var right any
			right, err = p.comparison()
			left = !modelViewerEqual(left, right)
			continue
		}
		break
	}
	return left, err
}

func (p *modelViewerExpressionParser) comparison() (any, error) {
	left, err := p.additive()
	for err == nil {
		operator := ""
		for _, candidate := range []string{"<", "<=", ">", ">="} {
			if p.match(candidate) {
				operator = candidate
				break
			}
		}
		if operator == "" {
			break
		}
		right, nextErr := p.additive()
		if nextErr != nil {
			return nil, nextErr
		}
		left, err = modelViewerCompare(left, right, operator)
	}
	return left, err
}

func (p *modelViewerExpressionParser) additive() (any, error) {
	left, err := p.multiplicative()
	for err == nil {
		if p.match("+") {
			right, nextErr := p.multiplicative()
			if nextErr != nil {
				return nil, nextErr
			}
			if _, ok := left.(string); ok {
				left = modelViewerString(left) + modelViewerString(right)
			} else if _, ok := right.(string); ok {
				left = modelViewerString(left) + modelViewerString(right)
			} else {
				a, aErr := modelViewerNumber(left)
				b, bErr := modelViewerNumber(right)
				if aErr != nil || bErr != nil {
					return nil, fmt.Errorf("expression value is not numeric")
				}
				left = a + b
			}
			continue
		}
		if p.match("-") {
			right, nextErr := p.multiplicative()
			if nextErr != nil {
				return nil, nextErr
			}
			a, aErr := modelViewerNumber(left)
			b, bErr := modelViewerNumber(right)
			if aErr != nil || bErr != nil {
				return nil, fmt.Errorf("expression value is not numeric")
			}
			left = a - b
			continue
		}
		break
	}
	return left, err
}

func (p *modelViewerExpressionParser) multiplicative() (any, error) {
	left, err := p.unary()
	for err == nil {
		operator := ""
		for _, candidate := range []string{"*", "/", "//", "%"} {
			if p.match(candidate) {
				operator = candidate
				break
			}
		}
		if operator == "" {
			break
		}
		right, nextErr := p.unary()
		if nextErr != nil {
			return nil, nextErr
		}
		a, aErr := modelViewerNumber(left)
		b, bErr := modelViewerNumber(right)
		if aErr != nil || bErr != nil {
			return nil, fmt.Errorf("expression value is not numeric")
		}
		switch operator {
		case "*":
			left = a * b
		case "/":
			left = a / b
		case "//":
			left = math.Floor(a / b)
		case "%":
			left = math.Mod(a, b)
		}
	}
	return left, err
}

func (p *modelViewerExpressionParser) unary() (any, error) {
	if p.match("!") {
		value, err := p.unary()
		return !modelViewerTruthy(value), err
	}
	if p.match("-") {
		value, err := p.unary()
		if err != nil {
			return nil, err
		}
		number, err := modelViewerNumber(value)
		return -number, err
	}
	if p.match("+") {
		value, err := p.unary()
		if err != nil {
			return nil, err
		}
		return modelViewerNumber(value)
	}
	return p.primary()
}

func (p *modelViewerExpressionParser) primary() (any, error) {
	if p.index >= len(p.tokens) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	token := p.tokens[p.index]
	p.index++
	switch token.kind {
	case "number", "string", "boolean":
		return token.value, nil
	case "identifier":
		if value, ok := p.variables[modelViewerNormalizeKey(token.text)]; ok {
			return value, nil
		}
		return float64(0), nil
	case "paren":
		if token.text != "(" {
			return nil, fmt.Errorf("unexpected closing parenthesis")
		}
		value, err := p.logicalOr()
		if err != nil {
			return nil, err
		}
		if p.index >= len(p.tokens) || p.tokens[p.index].kind != "paren" || p.tokens[p.index].text != ")" {
			return nil, fmt.Errorf("expected closing parenthesis")
		}
		p.index++
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected token type: %s", token.kind)
	}
}

func (p *modelViewerExpressionParser) match(operator string) bool {
	if p.index >= len(p.tokens) || p.tokens[p.index].kind != "operator" || p.tokens[p.index].text != operator {
		return false
	}
	p.index++
	return true
}

func modelViewerTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case float64:
		return typed != 0 && !math.IsNaN(typed)
	case string:
		return typed != ""
	default:
		return true
	}
}

func modelViewerNumber(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case int:
		return float64(typed), nil
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, nil
		}
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, fmt.Errorf("expression value is not numeric: %v", value)
	}
}

func modelViewerEqual(left, right any) bool {
	a, aErr := modelViewerNumber(left)
	b, bErr := modelViewerNumber(right)
	if aErr == nil && bErr == nil && !math.IsNaN(a) && !math.IsNaN(b) {
		return a == b
	}
	return modelViewerString(left) == modelViewerString(right)
}

func modelViewerCompare(left, right any, operator string) (bool, error) {
	a, aErr := modelViewerNumber(left)
	b, bErr := modelViewerNumber(right)
	if aErr == nil && bErr == nil && !math.IsNaN(a) && !math.IsNaN(b) {
		switch operator {
		case "<":
			return a < b, nil
		case "<=":
			return a <= b, nil
		case ">":
			return a > b, nil
		case ">=":
			return a >= b, nil
		}
	}
	aString, bString := modelViewerString(left), modelViewerString(right)
	switch operator {
	case "<":
		return aString < bString, nil
	case "<=":
		return aString <= bString, nil
	case ">":
		return aString > bString, nil
	case ">=":
		return aString >= bString, nil
	default:
		return false, fmt.Errorf("unsupported comparison operator %s", operator)
	}
}

func modelViewerString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}
