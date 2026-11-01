package modelviewer

import "math"

type modelViewerSymbolicNumericValue struct {
	value      float64
	conditions ModelViewerDNF
}

type modelViewerSymbolicNumber struct {
	values  []modelViewerSymbolicNumericValue
	unknown ModelViewerDNF
}

// Derived selectors keep their input conditions so transport normalization does
// not discard them as runtime-only variables and expose every mesh variant.
func (c *modelViewerSymbolicScanContext) recordDerivedAssignment(
	variable, expression string,
	conditions ModelViewerDNF,
) {
	resolved, ok := c.numericExpressionValues(expression)
	if !ok {
		if _, exists := c.derived[variable]; !exists {
			return
		}
		resolved = modelViewerSymbolicNumber{unknown: modelViewerDNFTrue()}
	}

	previous, exists := c.derived[variable]
	if !exists {
		previous, ok = c.numericExpressionValues("$" + variable)
		if !ok {
			previous = modelViewerSymbolicNumber{unknown: modelViewerDNFTrue()}
		}
	}
	remaining := modelViewerDNFNot(conditions)
	output := modelViewerSymbolicNumber{unknown: modelViewerDNFOr(
		modelViewerDNFAnd(previous.unknown, remaining),
		modelViewerDNFAnd(resolved.unknown, conditions),
	)}
	for _, value := range previous.values {
		value.conditions = modelViewerDNFAnd(value.conditions, remaining)
		if len(value.conditions) > 0 {
			output.values = append(output.values, value)
		}
	}
	for _, value := range resolved.values {
		value.conditions = modelViewerDNFAnd(value.conditions, conditions)
		if len(value.conditions) > 0 {
			output.values = append(output.values, value)
		}
	}
	if c.derived == nil {
		c.derived = make(map[string]modelViewerSymbolicNumber)
	}
	c.derived[variable] = output
}

func (c *modelViewerSymbolicScanContext) numericExpressionValues(
	expression string,
) (modelViewerSymbolicNumber, bool) {
	tokens, err := tokenizeModelViewerExpression(expression)
	if err != nil {
		return modelViewerSymbolicNumber{}, false
	}

	var names []string
	var domains [][]modelViewerSymbolicNumericValue
	seen := make(map[string]bool)
	combinations := 1
	unknown := modelViewerDNFFalse()
	for _, token := range tokens {
		if token.kind != "identifier" {
			continue
		}
		name := modelViewerNormalizeKey(token.text)
		if seen[name] {
			continue
		}
		seen[name] = true
		derived, exists := c.derived[name]
		values := derived.values
		if exists {
			unknown = modelViewerDNFOr(unknown, derived.unknown)
		} else if assigned, exists := c.assigned[name]; exists {
			values = []modelViewerSymbolicNumericValue{{value: assigned.value, conditions: modelViewerDNFTrue()}}
		} else if domain, exists := c.variables["__domain:"+name].([]any); exists {
			for _, item := range domain {
				number, numberErr := modelViewerNumber(item)
				if numberErr != nil {
					return modelViewerSymbolicNumber{}, false
				}
				values = append(values, modelViewerSymbolicNumericValue{
					value: number, conditions: ModelViewerDNF{{{Var: name, Value: modelViewerString(item)}}},
				})
			}
		}
		if len(values) == 0 || combinations > maxModelViewerDNFGroups/len(values) {
			return modelViewerSymbolicNumber{}, false
		}
		combinations *= len(values)
		names = append(names, name)
		domains = append(domains, values)
	}

	state := make(map[string]any, len(names))
	var output []modelViewerSymbolicNumericValue
	var visit func(int, ModelViewerDNF) bool
	visit = func(index int, conditions ModelViewerDNF) bool {
		if len(conditions) == 0 {
			return true
		}
		if index < len(names) {
			for _, value := range domains[index] {
				state[names[index]] = value.value
				if !visit(index+1, modelViewerDNFAnd(conditions, value.conditions)) {
					return false
				}
			}
			return true
		}
		parser := modelViewerExpressionParser{tokens: tokens, variables: state}
		result, evalErr := parser.logicalOr()
		number, numeric := result.(float64)
		if evalErr != nil || parser.index != len(tokens) || !numeric || math.IsNaN(number) || math.IsInf(number, 0) {
			return false
		}
		output = append(output, modelViewerSymbolicNumericValue{value: number, conditions: conditions})
		return true
	}
	ok := visit(0, modelViewerDNFTrue())
	return modelViewerSymbolicNumber{values: output, unknown: unknown}, ok
}

func foldModelViewerDerivedAtom(
	raw string,
	derived map[string]modelViewerSymbolicNumber,
) (ModelViewerDNF, bool) {
	match := modelViewerDNFClauseRE.FindStringSubmatch(raw)
	if match == nil {
		match = modelViewerDNFBareRE.FindStringSubmatch(raw)
	}
	if match == nil {
		return nil, false
	}
	name := modelViewerNormalizeKey(match[1])
	number, exists := derived[name]
	if !exists {
		return nil, false
	}
	output := modelViewerDNFFalse()
	for _, value := range number.values {
		folded, ok := foldModelViewerAssignedAtom(raw, map[string]modelViewerAssignedValue{name: {value: value.value}})
		if !ok {
			return nil, false
		}
		if modelViewerDNFIsTrue(folded) {
			output = modelViewerDNFOr(output, value.conditions)
		}
	}

	// An unresolved assignment invalidates only its active states. Keep the
	// original selector in uncovered states for runtime-guard normalization.
	unknown := modelViewerDNFAnd(number.unknown, modelViewerDNFAtom(raw, nil, nil))
	return modelViewerDNFOr(output, unknown), true
}
