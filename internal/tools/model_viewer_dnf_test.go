package tools

import (
	"reflect"
	"slices"
	"testing"
)

func TestModelViewerDNFRemovesRedundantExclusions(t *testing.T) {
	for _, expression := range []string{
		"$frame != 0 && $frame != 1 && $FRAME == 2 && $hat != 0",
		"$frame == 2 && $frame != 0 && $hat != 0 && $frame != 1",
		"$frame != 0 && $hat != 0 && $frame == 2 && $frame == 2",
	} {
		dnf := parseModelViewerConditionDNF(expression, nil, nil)
		if len(dnf) != 1 || len(dnf[0]) != 2 ||
			!slices.Contains(dnf[0], ModelViewerDNFClause{Var: "frame", Value: "2"}) ||
			!slices.Contains(dnf[0], ModelViewerDNFClause{Var: "hat", Value: "0", Negate: true}) {
			t.Fatalf("%s = %#v", expression, dnf)
		}
		for _, frame := range []string{"0", "1", "2", "3", ""} {
			for _, hat := range []string{"0", "1"} {
				state := map[string]string{"frame": frame, "hat": hat}
				if modelViewerDNFSatisfied(dnf, state) != (frame == "2" && hat != "0") {
					t.Fatalf("%s: state %v changed meaning", expression, state)
				}
			}
		}
	}
	for _, expression := range []string{
		"$frame != 2 && $frame == 2",
		"$frame == 2 && $frame != 2",
		"$frame != 0 && $frame == 2 && $frame == 3",
	} {
		if dnf := parseModelViewerConditionDNF(expression, nil, nil); len(dnf) != 0 {
			t.Fatalf("contradiction %s = %#v", expression, dnf)
		}
	}
}

func TestModelViewerDNFExclusionPreservesEmptyValue(t *testing.T) {
	input := []ModelViewerDNFClause{{Var: "frame", Value: "", Negate: true}}
	group, possible := simplifyModelViewerDNFGroup(input)
	if !possible || !slices.Equal(group, input) {
		t.Fatalf("empty-value exclusion = %#v, %t", group, possible)
	}
}

func TestModelViewerDNFDistinguishesTrueAndFalse(t *testing.T) {
	trueDNF := parseModelViewerConditionDNF("1", nil, nil)
	falseDNF := parseModelViewerConditionDNF("0", nil, nil)
	if !modelViewerDNFIsTrue(trueDNF) || len(falseDNF) != 0 || modelViewerDNFIsTrue(falseDNF) {
		t.Fatalf("true=%#v false=%#v", trueDNF, falseDNF)
	}
	if len(modelViewerDNFNot(trueDNF)) != 0 || !modelViewerDNFIsTrue(modelViewerDNFNot(falseDNF)) {
		t.Fatalf("not true=%#v not false=%#v", modelViewerDNFNot(trueDNF), modelViewerDNFNot(falseDNF))
	}
}

func TestModelViewerDNFParsesAndOrNot(t *testing.T) {
	dnf := parseModelViewerConditionDNF(`$outfit == 1 && ($hat != 0 || !$shoe)`, nil, nil)
	wanted := ModelViewerDNF{
		{{Var: "outfit", Value: "1"}, {Var: "hat", Value: "0", Negate: true}},
		{{Var: "outfit", Value: "1"}, {Var: "shoe", Value: "0"}},
	}
	if !reflect.DeepEqual(dnf, wanted) {
		t.Fatalf("dnf = %#v", dnf)
	}
}

func TestModelViewerDNFExpandsComparisonAndFalseDomain(t *testing.T) {
	variables := map[string]any{"__domain:top": []any{float64(0), float64(1), float64(2)}}
	low := parseModelViewerConditionDNF("$top < 2", nil, variables)
	if len(low) != 2 || low[0][0].Value != "0" || low[1][0].Value != "1" {
		t.Fatalf("low = %#v", low)
	}
	if impossible := parseModelViewerConditionDNF("$top > 5", nil, variables); len(impossible) != 0 {
		t.Fatalf("impossible = %#v", impossible)
	}
}

func TestModelViewerDNFResolvesBooleanAlias(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $top = 0
global persist $hat = 0
[KeyTop]
type = cycle
$top = 0,1
[KeyHat]
type = cycle
$hat = 0,1
[Present]
$visible = $top == 1 && $hat != 0`)
	variables := modelViewerDirectConditionVariables(sections, collectModelViewerDefaultVariables(sections))
	dnf := modelViewerConditionsToDNF([]modelViewerConditionClause{{Expression: "$visible", Expected: true}}, variables)
	wanted := ModelViewerDNF{{{Var: "top", Value: "1"}, {Var: "hat", Value: "0", Negate: true}}}
	if !reflect.DeepEqual(dnf, wanted) {
		t.Fatalf("dnf = %#v aliases=%#v", dnf, variables["__aliases"])
	}
}

func TestModelViewerDNFMatchesElectronAtomGrammar(t *testing.T) {
	alias := ModelViewerDNF{{{Var: "outfit", Value: "1"}}}
	aliases := map[string]ModelViewerDNF{"Visible": alias}

	if dnf := parseModelViewerConditionDNF("$outfit = 1", aliases, nil); !modelViewerDNFIsTrue(dnf) {
		t.Fatalf("single equals = %#v", dnf)
	}
	if dnf := parseModelViewerConditionDNF("$outfit.part == 1", aliases, nil); !modelViewerDNFIsTrue(dnf) {
		t.Fatalf("dotted variable = %#v", dnf)
	}
	if dnf := parseModelViewerConditionDNF("$Visible", aliases, nil); !reflect.DeepEqual(dnf, alias) {
		t.Fatalf("exact alias = %#v", dnf)
	}
	wantMismatchedCase := ModelViewerDNF{{{Var: "visible", Value: "0", Negate: true}}}
	if dnf := parseModelViewerConditionDNF("$visible", aliases, nil); !reflect.DeepEqual(dnf, wantMismatchedCase) {
		t.Fatalf("case-mismatched alias = %#v", dnf)
	}
}

func TestModelViewerDNFRemovesContradictoryGroup(t *testing.T) {
	left := ModelViewerDNF{{{Var: "top", Value: "0"}}}
	right := ModelViewerDNF{{{Var: "top", Value: "1"}}}
	if combined := modelViewerDNFAnd(left, right); len(combined) != 0 {
		t.Fatalf("combined = %#v", combined)
	}
}

func TestModelViewerDNFIntersectsBeyondMaterializationLimit(t *testing.T) {
	left := make(ModelViewerDNF, maxModelViewerDNFGroups+1)
	for index := range left {
		left[index] = []ModelViewerDNFClause{{Var: "frame", Value: modelViewerString(index)}}
	}
	if modelViewerDNFIntersects(left, ModelViewerDNF{{{Var: "frame", Value: "999"}}}) {
		t.Fatal("disjoint expressions must not intersect")
	}
	if !modelViewerDNFIntersects(
		left,
		ModelViewerDNF{{{Var: "frame", Value: modelViewerString(maxModelViewerDNFGroups)}}},
	) {
		t.Fatal("matching expression should intersect")
	}
}

func TestModelViewerDNFCoversAssignmentCondition(t *testing.T) {
	draw := ModelViewerDNF{{{Var: "color", Value: "1"}, {Var: "hair", Value: "0"}}}
	if !modelViewerDNFCovers(draw, ModelViewerDNF{{{Var: "color", Value: "1"}}}) {
		t.Fatal("draw condition should cover the color assignment")
	}
	if modelViewerDNFCovers(draw, ModelViewerDNF{{{Var: "color", Value: "2"}}}) {
		t.Fatal("draw condition must not cover a different color")
	}
	if modelViewerDNFCovers(modelViewerDNFTrue(), ModelViewerDNF{{{Var: "color", Value: "1"}}}) {
		t.Fatal("an unconstrained draw does not imply a conditional assignment")
	}
}

func TestNormalizeModelViewerDNFWithTrackedRemovesRuntimeGuards(t *testing.T) {
	dnf := ModelViewerDNF{{
		{Var: "mod_enabled", Value: "0", Negate: true},
		{Var: "outfit", Value: "1"},
	}}
	wanted := ModelViewerDNF{{{Var: "outfit", Value: "1"}}}
	if normalized := normalizeModelViewerDNFWithTracked(
		dnf,
		map[string]bool{"outfit": true},
	); !reflect.DeepEqual(
		normalized,
		wanted,
	) {
		t.Fatalf("normalized = %#v", normalized)
	}
	if normalized := normalizeModelViewerDNFWithTracked(dnf, nil); !modelViewerDNFIsTrue(normalized) {
		t.Fatalf("runtime-only condition = %#v", normalized)
	}
	if normalized := normalizeModelViewerDNFWithTracked(
		ModelViewerDNF{},
		map[string]bool{"outfit": true},
	); len(
		normalized,
	) != 0 {
		t.Fatalf("false condition = %#v", normalized)
	}
}
