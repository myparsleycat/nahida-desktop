package tools

import "testing"

func TestTouchAssetPrefixRemovesOnlyFirstPositionToken(t *testing.T) {
	t.Parallel()

	component := TouchComponentAnalysis{Name: "PositionHatPosition", Kind: "other"}
	if got := touchAssetPrefix(component, "Alice"); got != "AliceHatPosition" {
		t.Fatalf("touchAssetPrefix = %q, want %q", got, "AliceHatPosition")
	}
}

func TestModelViewerFamilyFallbackRequiresUppercaseVariantSuffix(t *testing.T) {
	t.Parallel()

	section := &familyRoleSection{name: "BodyADiffuse"}
	sections := map[string]*familyRoleSection{"bodyadiffuse": section}
	if got := lookupFamilyRoleSection(sections, "BodyB", "Diffuse"); got != section {
		t.Fatalf("uppercase section fallback = %#v, want %#v", got, section)
	}
	if got := lookupFamilyRoleSection(sections, "Bodyb", "Diffuse"); got != nil {
		t.Fatalf("lowercase section fallback = %#v, want nil", got)
	}

	resources := []modelViewerResource{{Name: "BodyADiffuse", Filename: "body.png"}}
	if got := lookupRoleResource("BodyBIB", "Diffuse", resources); got != "BodyADiffuse" {
		t.Fatalf("uppercase resource fallback = %q, want %q", got, "BodyADiffuse")
	}
	if got := lookupRoleResource("BodybIB", "Diffuse", resources); got != "" {
		t.Fatalf("lowercase resource fallback = %q, want empty", got)
	}
}

func TestModelViewerMenuBackreferenceKeepsPunctuationSignificant(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		`$a.b = 1 - $ab`,
		`$a.b = $ab + 1`,
		`$a.b = ($ab + 1) % 3`,
	} {
		if variable, _, _, ok := parseModelViewerMenuBranch([]string{line}); ok {
			t.Fatalf("%q matched different variable %q", line, variable)
		}
	}
}

func TestModelViewerShapePositionVariantValidatesSuffixCharacters(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"BodyPositionSmile", "BodyPositionSmile.1", "BodyPositionSmile-Alt"} {
		if !isModelViewerShapePositionVariant(name) {
			t.Fatalf("%q should be a shape position variant", name)
		}
	}
	for _, name := range []string{"BodyPosition", "BodyPositionBase", "BodyPositionBase.1", "BodyPositionSmile Extra", "BodyPositionSmile/Extra"} {
		if isModelViewerShapePositionVariant(name) {
			t.Fatalf("%q should not be a shape position variant", name)
		}
	}
}
