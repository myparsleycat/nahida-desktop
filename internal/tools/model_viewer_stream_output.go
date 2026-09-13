package tools

import (
	"bytes"
	"strconv"
	"strings"
)

type modelViewerStreamOutput struct {
	data   []byte
	stride int
}

// Ordered three-valued logic: false < unknown < true. min/max implement AND/OR.
type modelViewerStreamBranch struct {
	active, seen    int
	uncertain       bool
	positionChanged bool
	position        string
}

func modelViewerStreamCondition(expression string) int {
	tokens, err := tokenizeModelViewerExpression(expression)
	if err != nil {
		return 1
	}
	for _, token := range tokens {
		if token.kind == "identifier" {
			return 1
		}
	}
	value, err := evaluateModelViewerExpression(expression, nil)
	if err != nil {
		return 1
	}
	if modelViewerTruthy(value) {
		return 2
	}
	return 0
}

// Recover bind-pose positions for linear stream-output replay lists. GPU skinning
// is deliberately not emulated: draw order supplies the output vertex addresses.
// Only direct file-backed draws at the binding's branch depth are accepted.
func collectModelViewerStreamOutputs(
	modDir string,
	sections []modINISection,
	resources map[string]modelViewerResource,
	cache *modelViewerBufferCache,
) map[string]modelViewerStreamOutput {
	outputs := make(map[string]modelViewerStreamOutput)
	ambiguous := make(map[string]bool)
	aliases := make(map[string][]string)
	var allocated int64
	store := func(name string, stream modelViewerStreamOutput) {
		if previous, exists := outputs[name]; exists &&
			(previous.stride != stream.stride || !bytes.Equal(previous.data, stream.data)) {
			ambiguous[name] = true
		}
		outputs[name] = stream
	}
	for _, section := range sections {
		position, target := "", ""
		depth, bindDepth := 0, 0
		invalid := false
		stream := modelViewerStreamOutput{}
		var branches []modelViewerStreamBranch
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				depth++
				condition := modelViewerStreamCondition(strings.TrimSpace(line[3:]))
				branches = append(branches, modelViewerStreamBranch{
					active: condition, seen: condition, uncertain: condition == 1, position: position,
				})
				continue
			case lower == "endif":
				if len(branches) == 0 {
					continue
				}
				frame := branches[len(branches)-1]
				if frame.uncertain && frame.positionChanged {
					position = ""
				}
				branches = branches[:len(branches)-1]
				depth--
				continue
			case lower == "else" || strings.HasPrefix(lower, "else if ") || strings.HasPrefix(lower, "elif "):
				if len(branches) == 0 {
					continue
				}
				condition := 2
				if lower != "else" {
					expression := strings.TrimSpace(line[5:])
					if strings.HasPrefix(lower, "else if ") {
						expression = strings.TrimSpace(line[8:])
					}
					condition = modelViewerStreamCondition(expression)
				}
				frame := &branches[len(branches)-1]
				frame.active = min(2-frame.seen, condition)
				frame.seen = max(frame.seen, condition)
				frame.uncertain = frame.uncertain || frame.active == 1
				if frame.uncertain {
					position = frame.position
				}
				if target != "" && depth <= bindDepth {
					invalid = true
				}
				continue
			}
			active := 2
			for _, branch := range branches {
				active = min(active, branch.active)
			}
			if active == 0 {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
			resource := modelViewerNormalizeKey(modelViewerResourceToken(value))
			if strings.HasPrefix(key, "resource") && resource != "" &&
				(strings.HasPrefix(strings.ToLower(value), "copy ") || strings.HasPrefix(strings.ToLower(value), "ref ")) {
				name := modelViewerNormalizeKey(modelViewerResourceToken(key))
				aliases[name] = appendUniqueModelViewer(aliases[name], resource)
			}
			switch key {
			case "so0":
				if target != "" {
					if invalid || depth != bindDepth {
						ambiguous[target] = true
					} else if len(stream.data) > 0 {
						store(target, stream)
					}
				}
				target, bindDepth = resource, depth
				stream, invalid = modelViewerStreamOutput{}, false
			case "vb0":
				position = resource
				for index := range branches {
					branches[index].positionChanged = true
				}
				if target != "" && depth != bindDepth {
					invalid = true
				}
			case "run", "drawindexed", "drawindexedinstanced", "drawauto":
				if target != "" {
					invalid = true
				}
			case "draw":
				if target == "" || invalid {
					continue
				}
				args := strings.Split(value, ",")
				if len(args) != 2 || depth != bindDepth {
					invalid = true
					continue
				}
				count, countErr := strconv.Atoi(strings.TrimSpace(args[0]))
				start, startErr := strconv.Atoi(strings.TrimSpace(args[1]))
				input := resources[position]
				output := resources[target]
				if countErr != nil || startErr != nil || count <= 0 || start < 0 || input.Stride < 12 ||
					output.Stride != input.Stride || input.Filename == "" || output.Filename != "" ||
					(stream.stride != 0 && stream.stride != input.Stride) {
					invalid = true
					continue
				}
				data, readOK := readModelViewerObjectBuffer(modDir, input, cache)
				if !readOK || len(data)%input.Stride != 0 || start > len(data)/input.Stride ||
					count > len(data)/input.Stride-start ||
					int64(count)*int64(input.Stride) > maxModelViewerBufferFileBytes-int64(len(stream.data)) {
					invalid = true
					continue
				}
				if int64(count)*int64(input.Stride) > maxModelViewerTotalBufferBytes-allocated {
					invalid = true
					continue
				}
				allocated += int64(count) * int64(input.Stride)
				stream.stride = input.Stride
				stream.data = append(stream.data, data[start*input.Stride:(start+count)*input.Stride]...)
			}
		}
	}
	// Copies of the completed stream (including previous-frame buffers) retain
	// the same bind-pose layout. Cycles and conflicting sources stay unresolved.
	for range maxModelViewerResourceAliasDepth {
		changed := false
		for name, sources := range aliases {
			if _, exists := outputs[name]; exists || len(sources) != 1 || ambiguous[sources[0]] {
				continue
			}
			if stream, ok := outputs[sources[0]]; ok {
				outputs[name] = stream
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for name := range ambiguous {
		delete(outputs, name)
	}
	return outputs
}
