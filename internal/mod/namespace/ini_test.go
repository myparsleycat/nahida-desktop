package namespace

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

const qingxiaoINI = `; Qingxiao mini fixture
namespace = Creator\Qingxiao Outfit

[Constants]
global persist $dress = 0 ; user default
global persist $hair = 1
global $active = 0

[Present]
if $active == 1 && $\Shared GUI\visible
  run = CommandList\Creator\Qingxiao Outfit\Update
  Resource\Shared GUI\Preview = ref Resource\Creator\Qingxiao Outfit\Diffuse
  run = CustomShader\Shared GUI\Draw
endif

[ResourceDiffuse]
filename = Textures\Qingxiao Dress.dds
`

func TestParse(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte(qingxiaoINI))
	if err != nil || document.Unsupported {
		t.Fatalf("Parse = %+v, %v", document, err)
	}
	if document.Namespace != `Creator\Qingxiao Outfit` ||
		!slices.Equal(document.Persistent, []string{"dress", "hair"}) {
		t.Fatalf("unexpected declarations: %+v", document)
	}
	if !slices.Equal(document.References, []string{`Shared GUI`, `Creator\Qingxiao Outfit`}) {
		t.Fatalf("references = %q", document.References)
	}
	normalized, err := Normalize([]byte(qingxiaoINI), nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(normalized))
	if document.Fingerprint != hex.EncodeToString(hash[:]) {
		t.Fatalf("fingerprint does not hash Normalize: %q", document.Fingerprint)
	}
}

func TestParseReferences(t *testing.T) {
	t.Parallel()
	input := `NaMeSpAcE = Qingxiao Copy ; comment
[Constants]
GLOBAL PERSIST $Dress = 0
[Present]
if $\GUI One\state == $\GUI Two\state
Resource\Creator A\GUI\Preview = ref Resource\Creator B\GUI\Preview
run = CommandList\global\ORFix\ORFix
run = CustomShader\GUI One\Draw
run = Preset\GUI Two\Mode
run = Key\Controls\Toggle
run = ShaderRegex\Regex Library\Match
run = TextureOverride\Texture Library\Draw
run = ShaderOverride\Shader Library\Draw
; $\Fake\variable
text = "$\Quoted GUI\state"
`
	document, err := Parse([]byte(input))
	want := []string{"GUI One", "GUI Two", `Creator A\GUI`, `Creator B\GUI`, `global\ORFix`,
		"Controls", "Regex Library", "Texture Library", "Shader Library"}
	if err != nil || document.Unsupported || !slices.Equal(document.References, want) {
		t.Fatalf("Parse = %+v, %v; want references %q", document, err, want)
	}
}

func TestParseQingxiaoMainAndGUI(t *testing.T) {
	t.Parallel()
	// Use the Qingxiao namespace and persistent identifiers from the repository's
	// toggle-persistence fixtures, including the hyphenated namespace segments.
	main := `namespace = Mods\QingXiao-FeiHuaLing
[Constants]
global persist $xiezi_f1 = 0
global $active = 0
[Present]
if $active == 1
run = CommandList\Mods\QingXiao-FeiHuaLing\DrawGUI
endif
[ResourcePreview]
filename = GUI\QingXiao Preview.dds
`
	gui := `namespace = Mods\QingXiao-FeiHuaLing
[Constants]
global persist $xiezi_f2 = 0.25
[CommandListDrawGUI]
if $\Mods\QingXiao-FeiHuaLing\xiezi_f1 == 1
Resource\Shared GUI\Preview = ref Resource\Mods\QingXiao-FeiHuaLing\Preview
endif
`
	for _, tt := range []struct {
		name       string
		input      string
		persistent string
		references []string
	}{
		{name: "main", input: main, persistent: "xiezi_f1", references: []string{`Mods\QingXiao-FeiHuaLing`}},
		{name: "gui", input: gui, persistent: "xiezi_f2", references: []string{`Mods\QingXiao-FeiHuaLing`, "Shared GUI"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			document, err := Parse([]byte(tt.input))
			if err != nil || document.Unsupported || document.Namespace != `Mods\QingXiao-FeiHuaLing` ||
				!slices.Equal(
					document.Persistent,
					[]string{tt.persistent},
				) || !slices.Equal(document.References, tt.references) {
				t.Fatalf("Qingxiao fixture = %+v, %v", document, err)
			}
			updated, err := Rewrite([]byte(tt.input), map[string]string{
				`Mods\QingXiao-FeiHuaLing`: `Mods\QingXiao-FeiHuaLing__nhd_copy`,
			})
			if err != nil {
				t.Fatal(err)
			}
			copyDocument, err := Parse(updated)
			if err != nil || copyDocument.Unsupported ||
				copyDocument.Namespace != `Mods\QingXiao-FeiHuaLing__nhd_copy` {
				t.Fatalf("rewritten Qingxiao fixture = %+v, %v", copyDocument, err)
			}
		})
	}
}

func TestParseQingxiaoWWMISyntax(t *testing.T) {
	t.Parallel()
	// Reduced from the actual Qingxiao mod.ini: semicolon key bindings and WWMI
	// pool queries previously made this otherwise supported document unsafe.
	input := `namespace = Mods\QingXiao-FeiHuaLing
[Constants]
global persist $miansha_f1 = 1
global persist $miansha_f2 = 0
global $instance_id = 0
[KeymianshaF1]
condition = $object_detected && $mod_enabled && $form == 1
key = ;
type = cycle
$miansha_f1 = 0,1
[KeymianshaF2]
key = ;
$miansha_f2 = 0,1
[CommandListUpdateMergedSkeleton]
PoolMergeStatus_0[*] = 0
PoolMergedSkeleton[$PoolInstanceID[0]] = copy PoolMergedSkeletonRW[$PoolInstanceID[0]]
[CommandListCleanupSharedResources]
if ResourceBlendBufferOverride !== null
ResourceBlendBufferOverride = null
endif
[TextureOverrideComponent0]
if #PoolMergedSkeleton[$instance_id] !== -1
ps-t4 == null
if ps-t1->format == 99 && 1
$\WWMIv1\vg_offset = 0
run = CustomShader\WWMIv1\SkeletonMerger
endif
endif
[ResourceTextureComponents-0_t_263b6f70]
filename = Textures/Components-0_t_263b6f70.dds
`
	document, err := Parse([]byte(input))
	if err != nil || document.Unsupported || len(document.Persistent) != 2 ||
		!slices.Equal(document.References, []string{"WWMIv1"}) {
		t.Fatalf("actual Qingxiao syntax = %+v, %v", document, err)
	}
	canonical, err := Normalize([]byte(input), nil)
	if err != nil || !strings.Contains(canonical, "key = ;") ||
		!strings.Contains(canonical, "if #PoolMergedSkeleton [ $instance_id ] !== - 1") ||
		!strings.Contains(canonical, "ps - t4 == null") {
		t.Fatalf("key or pool code lost: %q, %v", canonical, err)
	}
	changedQuery := strings.Replace(
		input,
		"#PoolMergedSkeleton[$instance_id] !== -1",
		"#PoolMergedSkeleton[$instance_id] !== 0",
		1,
	)
	next, err := Parse([]byte(changedQuery))
	if err != nil || next.Unsupported || next.Fingerprint == document.Fingerprint {
		t.Fatalf("pool expression was treated as a comment: %+v, %v", next, err)
	}
	got, err := Rewrite([]byte(input), map[string]string{`Mods\QingXiao-FeiHuaLing`: "Copy"})
	want := strings.Replace(input, `namespace = Mods\QingXiao-FeiHuaLing`, "namespace = Copy", 1)
	if err != nil || string(got) != want {
		t.Fatalf("Rewrite changed key/pool code: %q, %v", got, err)
	}
}

func TestParseQualifiedPools(t *testing.T) {
	t.Parallel()
	input := `[Present]
if #Pool\Shared GUI\Skeleton[$index] !== -1 && $Pool\Shared GUI\State[$index] === 0
Pool\Shared GUI\Skeleton[$index] = copy ResourceLocal
endif
#Pool comment at the start of a line stays a comment
text = "#Pool\Ignored\Skeleton"
`
	document, err := Parse([]byte(input))
	if err != nil || document.Unsupported || !slices.Equal(document.References, []string{"Shared GUI"}) {
		t.Fatalf("qualified pools = %+v, %v", document, err)
	}
	got, err := Rewrite([]byte(input), map[string]string{"shared gui": "GUI Copy"})
	want := strings.ReplaceAll(input, `\Shared GUI\`, `\GUI Copy\`)
	if err != nil || string(got) != want {
		t.Fatalf("qualified pool rewrite = %q, %v", got, err)
	}
}

func TestParseLiteralCommentKeys(t *testing.T) {
	t.Parallel()
	for _, binding := range []string{";", "#"} {
		t.Run(binding, func(t *testing.T) {
			t.Parallel()
			input := "namespace = Original\r\n[KeyToggle]\r\nkey = " + binding + "\r\nrun = CommandListLocal\r\n"
			document, err := Parse([]byte(input))
			if err != nil || document.Unsupported {
				t.Fatalf("literal binding rejected: %+v, %v", document, err)
			}
			canonical, err := Normalize([]byte(input), nil)
			if err != nil || !strings.Contains(canonical, "key = "+binding) {
				t.Fatalf("literal binding lost: %q, %v", canonical, err)
			}
			other, err := Parse([]byte(strings.Replace(input, "key = "+binding, "key = VK_DOWN", 1)))
			if err != nil || document.Fingerprint == other.Fingerprint {
				t.Fatalf("different bindings compare equal: %+v, %v", other, err)
			}
			empty, err := Parse([]byte(strings.Replace(input, "key = "+binding, "key =", 1)))
			if err != nil || !empty.Unsupported || empty.Fingerprint == document.Fingerprint {
				t.Fatalf("empty binding accepted or equivalent: %+v, %v", empty, err)
			}
			got, err := Rewrite([]byte(input), map[string]string{"Original": "Copy"})
			want := strings.Replace(input, "namespace = Original", "namespace = Copy", 1)
			if err != nil || string(got) != want {
				t.Fatalf("namespace rewrite changed keyboard binding: %q, %v", got, err)
			}
		})
	}
	semicolon, err := Parse([]byte("[KeyToggle]\nkey = ;\n"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := Parse([]byte("[KeyToggle]\nkey = #\n"))
	if err != nil || semicolon.Fingerprint == hash.Fingerprint {
		t.Fatalf("semicolon and hash bindings compare equal: %+v, %v", hash, err)
	}
}

func TestParseUnsupported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "include section", input: "namespace = A\n[Include]\ninclude = Child\\mod.ini\n"},
		{name: "include directive", input: "namespace = A\n[Present]\ninclude = Child.ini\n"},
		{name: "duplicate namespace", input: "namespace = A\nnamespace = A\n[Constants]\n"},
		{name: "namespace in section", input: "[Constants]\nnamespace = A\n"},
		{name: "quoted namespace", input: "namespace = \"A\"\n"},
		{name: "empty namespace segment", input: "namespace = A\\\\B\n"},
		{name: "incomplete section", input: "[Constants\n"},
		{name: "unterminated string", input: "[Present]\ntext = \"hello\n"},
		{name: "unknown qualified prefix", input: "[Present]\nrun = Mystery\\A\\B\n"},
		{name: "incomplete reference", input: "[Present]\nrun = Resource\\A\n"},
		{name: "persistent outside constants", input: "[Present]\nglobal persist $x = 1\n"},
		{name: "duplicate persistent variable", input: "[Constants]\nglobal persist $x = 0\nglobal persist $X = 1\n"},
		{name: "qualified declaration", input: "[Constants]\nglobal persist $\\A\\x = 0\n"},
		{name: "empty initializer", input: "[Constants]\nglobal persist $x =\n"},
		{name: "empty assignment key", input: "[Present]\n= 1\n"},
		{name: "unknown preamble", input: "some directive\n[Constants]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := []byte(tt.input)
			document, err := Parse(data)
			if err != nil || !document.Unsupported || document.Fingerprint != "" {
				t.Fatalf("Parse = %+v, %v", document, err)
			}
			if result, err := Rewrite(data, map[string]string{"A": "B"}); err == nil || result != nil {
				t.Fatalf("Rewrite did not fail closed: %q, %v", result, err)
			}
			if result, err := Normalize(data, nil); err == nil || result != "" {
				t.Fatalf("Normalize did not fail closed: %q, %v", result, err)
			}
		})
	}
}

func TestParseIncludes(t *testing.T) {
	t.Parallel()
	input := `; importer configuration
namespace = Importer
[iNcLuDe]
InClUdE = Core\WWMI\main.ini ; core script
include = " Core\WWMI\GUI  Library.ini " # quoted path
include = '..\Unknown Scripts\outside.ini'
include = "C:\Other Root\script;#name.ini"
include = Core\WWMI\main.ini
include_recursive = Mods
INCLUDE_RECURSIVE = " D:\External Mods "
exclude_recursive = DISABLED*
; include = ignored.ini
include_other = ignored.ini
[Present]
include = NotAnIncludeSection.ini
`
	wantIncludes := []string{
		`Core\WWMI\main.ini`, `Core\WWMI\GUI  Library.ini`, `..\Unknown Scripts\outside.ini`,
		`C:\Other Root\script;#name.ini`, `Core\WWMI\main.ini`, "NotAnIncludeSection.ini",
	}
	wantRecursive := []string{"Mods", `D:\External Mods`}
	for _, encoding := range []string{"utf8", "utf8 bom", "utf16 le", "utf16 be"} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()
			data := encodeINITest(strings.ReplaceAll(input, "\n", "\r\n"), encoding)
			document, err := Parse(data)
			if err != nil || !document.Unsupported || document.Fingerprint != "" || document.Namespace != "Importer" ||
				!slices.Equal(
					document.Includes,
					wantIncludes,
				) || !slices.Equal(document.RecursiveIncludes, wantRecursive) {
				t.Fatalf("include inventory = %+v, %v", document, err)
			}
			if len(document.References) != 0 {
				t.Fatalf("include paths became namespace references: %q", document.References)
			}
			if _, err := Rewrite(data, map[string]string{"Importer": "Copy"}); err == nil {
				t.Fatal("include inventory permitted rewriting")
			}
			if _, err := Normalize(data, nil); err == nil {
				t.Fatal("include inventory permitted normalization")
			}
		})
	}
}

func TestParseIncludeExpressions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "dynamic expression", input: `include = $root + "\script.ini" ; trailing comment`,
			want: []string{`$root + "\script.ini"`}},
		{name: "environment expression", input: `include = %UNKNOWN_ROOT%\script.ini`,
			want: []string{`%UNKNOWN_ROOT%\script.ini`}},
		{name: "quoted concatenation", input: `include = "Core" + "\script.ini"`,
			want: []string{`"Core" + "\script.ini"`}},
		{name: "unterminated quote", input: `include = "Unknown Root\script.ini`,
			want: []string{`"Unknown Root\script.ini`}},
		{name: "trailing expression", input: `include = Core\script.ini = other.ini`,
			want: []string{`Core\script.ini = other.ini`}},
		{name: "empty value", input: "include = ; comment", want: []string{}},
		{name: "empty quoted value", input: `include = " "`, want: []string{}},
		{name: "missing assignment", input: `include Unknown Root\script.ini`, want: []string{}},
		{name: "wrong assignment", input: `include == Core\script.ini`, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			document, err := Parse([]byte("[Include]\n" + tt.input))
			if err != nil || !document.Unsupported || !slices.Equal(document.Includes, tt.want) {
				t.Fatalf("include expression = %+v, %v; want %q", document, err, tt.want)
			}
		})
	}
}

func TestParseCorePreambleInclude(t *testing.T) {
	t.Parallel()
	input := `namespace = WWMIv1
; Relative to Core\WWMI\WuWa-Model-Importer.ini, not the importer root.
[IncludeUtilities]
include = WWMI-Utilities.ini
include_recursive = " ..\External Scripts "
[Constants]
global persist $enabled = 1
`
	document, err := Parse([]byte(input))
	if err != nil || !document.Unsupported || document.Namespace != "WWMIv1" ||
		!slices.Equal(document.Includes, []string{"WWMI-Utilities.ini"}) ||
		!slices.Equal(document.RecursiveIncludes, []string{`..\External Scripts`}) ||
		!slices.Equal(document.Persistent, []string{"enabled"}) {
		t.Fatalf("Core preamble include = %+v, %v", document, err)
	}
}

func TestParseIncludeKeysInAnySection(t *testing.T) {
	t.Parallel()
	input := `include = Preamble.ini
[IncludeUtilities]
include = WWMI-Utilities.ini
[Present]
include = ..\Unknown Scripts\script.ini
include_recursive = " ..\External Root "
[IncludeAnother]
include_recursive = Core\WWMI
`
	document, err := Parse([]byte(input))
	if err != nil || !document.Unsupported ||
		!slices.Equal(
			document.Includes,
			[]string{"Preamble.ini", "WWMI-Utilities.ini", `..\Unknown Scripts\script.ini`},
		) ||
		!slices.Equal(document.RecursiveIncludes, []string{`..\External Root`, `Core\WWMI`}) {
		t.Fatalf("include keys hidden by section names: %+v, %v", document, err)
	}
}

func TestRewrite(t *testing.T) {
	t.Parallel()
	input := `namespace = Creator\Qingxiao Outfit ; keep
[Present]
run = CommandList\Creator\Qingxiao Outfit\Draw
run = CustomShader\Creator\Qingxiao Outfit\Draw
if $\creator\qingxiao outfit\dress == $\CreatorExtra\dress
Resource\Creator\Qingxiao Outfit\Preview = ref Resource\Creator\Other\Texture
endif
text = "Resource\Creator\Qingxiao Outfit\Draw ; untouched  string"
; $\Creator\Qingxiao Outfit\dress
[ResourceTexture]
filename = Resource\Creator\Qingxiao Outfit\Texture.dds
[CustomShaderDraw]
ps = Creator\Shader File.hlsl
`
	want := strings.Replace(input, `namespace = Creator\Qingxiao Outfit`, `namespace = New Outfit`, 1)
	want = strings.Replace(
		want,
		`run = CommandList\Creator\Qingxiao Outfit\Draw`,
		`run = CommandList\New Outfit\Draw`,
		1,
	)
	want = strings.Replace(
		want,
		`run = CustomShader\Creator\Qingxiao Outfit\Draw`,
		`run = CustomShader\New Outfit\Draw`,
		1,
	)
	want = strings.Replace(want, `$\creator\qingxiao outfit\dress`, `$\New Outfit\dress`, 1)
	want = strings.Replace(want, `Resource\Creator\Qingxiao Outfit\Preview`, `Resource\New Outfit\Preview`, 1)
	got, err := Rewrite([]byte(input), map[string]string{
		"Creator": "Root Copy", `CREATOR\QINGXIAO OUTFIT`: "New Outfit", "New Outfit": "Must Not Cascade",
	})
	if err != nil || string(got) != want {
		t.Fatalf("Rewrite = %q, %v; want %q", got, err, want)
	}
}

func TestRewriteEncoding(t *testing.T) {
	t.Parallel()
	input := "; 青霄 😀\r\nnamespace = 青霄 Outfit\r\n[Constants]\nglobal persist $dress = 0\r" +
		"[Present]\r\nrun = CommandList\\青霄 Outfit\\Draw ; stay"
	want := strings.ReplaceAll(input, "青霄 Outfit", "New Outfit")
	for _, encoding := range []string{"utf8", "utf8 bom", "utf16 le", "utf16 be"} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()
			data := encodeINITest(input, encoding)
			unchanged, err := Rewrite(data, nil)
			if err != nil || !bytes.Equal(data, unchanged) {
				t.Fatalf("no-op changed bytes: %x, %v", unchanged, err)
			}
			got, err := Rewrite(data, map[string]string{"青霄 Outfit": "New Outfit"})
			if err != nil || !bytes.Equal(got, encodeINITest(want, encoding)) {
				t.Fatalf("encoding/line preservation failed: %x, %v", got, err)
			}
			original, err := Parse(data)
			if err != nil || original.Unsupported {
				t.Fatalf("Parse = %+v, %v", original, err)
			}
			plain, err := Parse([]byte(input))
			if err != nil || original.Fingerprint != plain.Fingerprint {
				t.Fatalf("encoding affects fingerprint: %+v, %v", original, err)
			}
		})
	}
}

func TestRewriteNamespaceBoundaries(t *testing.T) {
	t.Parallel()
	input := `namespace = GUI One\Panel
[Present]
$\GUI One\Panel\state = $\GUI One Extra\state + $\GUI One\state
run = CommandList\GUI One\Panel\Draw
run = CommandList\GUI Two\Draw
text = 'CommandList\GUI One\Draw; quoted  value'
text2 = "doubled ""quote"" $\GUI One\state"
# $\GUI One\state
`
	want := `namespace = Panel Copy
[Present]
$\Panel Copy\state = $\GUI One Extra\state + $\GUI Copy\state
run = CommandList\Panel Copy\Draw
run = CommandList\GUI Two\Draw
text = 'CommandList\GUI One\Draw; quoted  value'
text2 = "doubled ""quote"" $\GUI One\state"
# $\GUI One\state
`
	got, err := Rewrite([]byte(input), map[string]string{"gui one": "GUI Copy", `GUI ONE\PANEL`: "Panel Copy"})
	if err != nil || string(got) != want {
		t.Fatalf("Rewrite = %q, %v; want %q", got, err, want)
	}
	document, err := Parse([]byte(input))
	if err != nil || document.Unsupported || !slices.Equal(document.References,
		[]string{`GUI One\Panel`, "GUI One Extra", "GUI One", "GUI Two"}) {
		t.Fatalf("namespace boundaries lost: %+v, %v", document, err)
	}
}

func TestRewriteExactNamespaces(t *testing.T) {
	t.Parallel()
	input := `namespace = Foo
[Present]
$\Foo\x = $\Foo\Bar\x + $\Foobar\x
run = CommandList\Foo\Draw
run = CommandList\Foo\Bar\Draw
Resource\Foo\Bar\Texture = ref Resource\Foo\Texture
`
	want := `namespace = New
[Present]
$\New\x = $\Foo\Bar\x + $\Foobar\x
run = CommandList\New\Draw
run = CommandList\Foo\Bar\Draw
Resource\Foo\Bar\Texture = ref Resource\New\Texture
`
	got, err := Rewrite([]byte(input), map[string]string{"fOO": "New"})
	if err != nil || string(got) != want {
		t.Fatalf("parent mapping changed a child namespace: %q, %v", got, err)
	}
	child, err := Rewrite([]byte(input), map[string]string{"foo": "New", `Foo\Bar`: "Child"})
	wantChild := strings.ReplaceAll(want, `\Foo\Bar\`, `\Child\`)
	if err != nil || string(child) != wantChild {
		t.Fatalf("explicit child mapping failed: %q, %v", child, err)
	}
	document, err := Parse([]byte(input))
	if err != nil || document.Unsupported || !slices.Equal(document.References, []string{"Foo", `Foo\Bar`, "Foobar"}) {
		t.Fatalf("exact reference namespaces = %+v, %v", document, err)
	}
	canonical, err := Normalize([]byte(input), map[string]string{"FOO": "New"})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := Normalize([]byte(want), nil)
	if err != nil || canonical != expected {
		t.Fatalf("Normalize changed child namespace: %q, %q, %v", canonical, expected, err)
	}
}

func TestParseWithoutExplicitNamespace(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "; comment only\r\n", "[Constants]\nglobal persist $dress\n"} {
		document, err := Parse([]byte(input))
		if err != nil || document.Unsupported || document.Namespace != "" || document.Fingerprint == "" {
			t.Fatalf("Parse = %+v, %v", document, err)
		}
		unchanged, err := Rewrite([]byte(input), nil)
		if err != nil || string(unchanged) != input {
			t.Fatalf("no-op = %q, %v", unchanged, err)
		}
	}
	a, err := Normalize([]byte("[Constants]\nglobal persist $dress\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize([]byte("[Constants]\nglobal persist $dress = 9\n"), nil)
	if err != nil || a != b {
		t.Fatalf("implicit persistent default differs: %q, %q, %v", a, b, err)
	}
}

func TestInvalidEncoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
	}{
		{name: "invalid utf8", data: []byte{0x80}},
		{name: "legacy code page", data: []byte("namespace = Caf\xe9\n")},
		{name: "bomless utf16", data: []byte{'[', 0, 'A', 0, ']', 0}},
		{name: "utf32 le", data: []byte{0xff, 0xfe, 0, 0, 'A', 0, 0, 0}},
		{name: "utf32 be", data: []byte{0, 0, 0xfe, 0xff, 0, 0, 0, 'A'}},
		{name: "truncated utf16", data: []byte{0xff, 0xfe, 'A'}},
		{name: "high surrogate", data: []byte{0xff, 0xfe, 0, 0xd8}},
		{name: "low surrogate", data: []byte{0xfe, 0xff, 0xdc, 0}},
		{name: "nul", data: []byte("[Constants]\n\x00")},
		{name: "interior bom", data: []byte("[Constants]\n\ufeff")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if document, err := Parse(tt.data); err == nil || !document.Unsupported {
				t.Fatalf("Parse accepted encoding: %+v, %v", document, err)
			}
			if _, err := Rewrite(tt.data, nil); err == nil {
				t.Fatal("Rewrite accepted invalid encoding")
			}
			if _, err := Normalize(tt.data, nil); err == nil {
				t.Fatal("Normalize accepted invalid encoding")
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	copyINI := strings.ReplaceAll(qingxiaoINI, `Creator\Qingxiao Outfit`, `Copy\Qingxiao Outfit`)
	copyINI = strings.Replace(copyINI, "$dress = 0 ; user default", "$dress=9", 1)
	copyINI = strings.Replace(copyINI, "$hair = 1", "$hair = 42", 1)
	copyINI = strings.ReplaceAll(copyINI, " == ", "==")
	copyINI = strings.ReplaceAll(copyINI, "\n", "\r\n")
	a, err := Normalize([]byte(qingxiaoINI), map[string]string{`creator\qingxiao outfit`: "Canonical"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize([]byte(copyINI), map[string]string{`COPY\QINGXIAO OUTFIT`: "Canonical"})
	if err != nil || a != b {
		t.Fatalf("copies differ:\n%s\n%s\n%v", a, b, err)
	}
	first, err := Parse([]byte(qingxiaoINI))
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := Parse([]byte(strings.Replace(qingxiaoINI, "$dress = 0", "$dress = 99", 1)))
	if err != nil || defaults.Fingerprint != first.Fingerprint {
		t.Fatalf("persistent defaults affect fingerprint: %+v, %v", defaults, err)
	}
}

func TestNormalizeMeaningfulDifferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		first string
		next  string
	}{
		{name: "quoted whitespace", first: `text = "Hello  World"`, next: `text = "Hello World"`},
		{name: "quoted case", first: `text = "Hello"`, next: `text = "hello"`},
		{name: "quoted punctuation", first: `text = "a;b#c"`, next: `text = "a"`},
		{name: "token boundary", first: "value = a b", next: "value = ab"},
		{name: "operator boundary", first: "if $a = = 1", next: "if $a == 1"},
		{name: "filename case", first: `filename = Folder\Texture.dds`, next: `filename = Folder\texture.dds`},
		{name: "filename spaces", first: `filename = Folder\Two  Words.dds`, next: `filename = Folder\Two Words.dds`},
		{name: "runtime assignment", first: "$dress = 0", next: "$dress = 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, err := Normalize([]byte("[Present]\n"+tt.first), nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Normalize([]byte("[Present]\n"+tt.next), nil)
			if err != nil || a == b {
				t.Fatalf("meaning lost: %q == %q, %v", a, b, err)
			}
		})
	}
}

func TestInvalidReplacements(t *testing.T) {
	t.Parallel()
	for _, mapping := range []map[string]string{
		{"A": ""}, {"": "B"}, {"A": "B\nnamespace = C"}, {"A": "B", "a": "C"}, {"A": `B\\C`},
	} {
		if _, err := Rewrite([]byte("namespace = A\n"), mapping); err == nil {
			t.Fatalf("Rewrite accepted %q", mapping)
		}
		if _, err := Normalize([]byte("namespace = A\n"), mapping); err == nil {
			t.Fatalf("Normalize accepted %q", mapping)
		}
	}
}

func encodeINITest(text, encoding string) []byte {
	if encoding == "utf8" {
		return []byte(text)
	}
	if encoding == "utf8 bom" {
		return append([]byte{0xef, 0xbb, 0xbf}, []byte(text)...)
	}
	units := utf16.Encode([]rune(text))
	data := make([]byte, 2+2*len(units))
	var order binary.ByteOrder = binary.LittleEndian
	data[0], data[1] = 0xff, 0xfe
	if encoding == "utf16 be" {
		order = binary.BigEndian
		data[0], data[1] = 0xfe, 0xff
	}
	for i, unit := range units {
		order.PutUint16(data[2+2*i:], unit)
	}
	return data
}
