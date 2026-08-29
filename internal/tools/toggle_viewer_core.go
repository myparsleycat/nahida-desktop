package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultToggleViewerHotkey = "ctrl H"

type toggleINIEntry struct {
	key   string
	value string
}

type toggleINISection struct {
	name    string
	entries []toggleINIEntry
}

type toggleKeySection struct {
	sectionName string
	keyValue    string
	backValue   string
}

type toggleViewerArtifact struct {
	targetINIPath string
	toggleTXTPath string
	toggleINIPath string
	toggleTXTHash string
	toggleINIHash string
	txtContent    string
	iniContent    string
}

var toggleSectionRE = regexp.MustCompile(`^\[(.+)\]$`)

func parseToggleINI(content string) []toggleINISection {
	var sections []toggleINISection
	current := -1
	for _, raw := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if match := toggleSectionRE.FindStringSubmatch(trimmed); len(match) == 2 {
			sections = append(sections, toggleINISection{name: strings.TrimSpace(match[1])})
			current = len(sections) - 1
			continue
		}
		if current < 0 {
			continue
		}
		separator := strings.Index(trimmed, "=")
		if separator <= 0 {
			continue
		}
		sections[current].entries = append(sections[current].entries, toggleINIEntry{
			key: strings.TrimSpace(trimmed[:separator]), value: strings.TrimSpace(trimmed[separator+1:]),
		})
	}
	return sections
}

func findToggleKeySections(sections []toggleINISection) []toggleKeySection {
	var out []toggleKeySection
	for _, section := range sections {
		if !strings.HasPrefix(strings.ToLower(section.name), "key") || !strings.EqualFold(toggleEntryValue(section, "type"), "cycle") {
			continue
		}
		hasMultiValue := false
		for _, entry := range section.entries {
			if !strings.HasPrefix(entry.key, "$") {
				continue
			}
			count := 0
			for _, value := range strings.Split(entry.value, ",") {
				if strings.TrimSpace(value) != "" {
					count++
				}
			}
			if count >= 2 {
				hasMultiValue = true
				break
			}
		}
		key := toggleEntryValue(section, "key")
		if !hasMultiValue || key == "" {
			continue
		}
		out = append(out, toggleKeySection{sectionName: section.name, keyValue: key, backValue: toggleEntryValue(section, "back")})
	}
	return out
}

func resolveTogglePositionHash(sections []toggleINISection) string {
	var textureSections, resourceSections []toggleINISection
	for _, section := range sections {
		lower := strings.ToLower(section.name)
		if strings.HasPrefix(lower, "textureoverride") {
			textureSections = append(textureSections, section)
		}
		if strings.HasPrefix(lower, "resource") {
			resourceSections = append(resourceSections, section)
		}
	}
	for _, section := range textureSections {
		if strings.Contains(strings.ToLower(section.name), "bodyposition") {
			if hash := toggleEntryValue(section, "hash"); hash != "" {
				return hash
			}
		}
	}
	bodyPositionResources := toggleResourceNameSet(resourceSections, "bodyposition")
	if len(bodyPositionResources) > 0 {
		if hash := hashForReferencedResource(textureSections, bodyPositionResources, false); hash != "" {
			return hash
		}
	}
	positionResources := toggleResourceNameSet(resourceSections, "position")
	if len(positionResources) > 0 {
		if hash := hashForReferencedResource(textureSections, positionResources, true); hash != "" {
			return hash
		}
	}
	for _, section := range resourceSections {
		if !strings.Contains(strings.ToLower(section.name), "position") {
			continue
		}
		for _, texture := range textureSections {
			if strings.EqualFold(toggleEntryValue(texture, "vb0"), section.name) {
				if hash := toggleEntryValue(texture, "hash"); hash != "" {
					return hash
				}
			}
		}
		break
	}
	for _, section := range sections {
		if !strings.EqualFold(section.name, "CommandListOverrideSharedResources") || !strings.Contains(strings.ToLower(toggleEntryValue(section, "vb0")), "position") {
			continue
		}
		for _, texture := range textureSections {
			if strings.Contains(strings.ToLower(texture.name), "component") {
				if hash := toggleEntryValue(texture, "hash"); hash != "" {
					return hash
				}
			}
		}
	}
	return ""
}

func toggleResourceNameSet(sections []toggleINISection, fragment string) map[string]bool {
	out := make(map[string]bool)
	for _, section := range sections {
		if strings.Contains(strings.ToLower(section.name), fragment) {
			out[strings.ToLower(section.name)] = true
		}
	}
	return out
}

func hashForReferencedResource(textures []toggleINISection, names map[string]bool, bodyOnly bool) string {
	for _, section := range textures {
		if bodyOnly && !strings.Contains(strings.ToLower(section.name), "body") {
			continue
		}
		if names[strings.ToLower(toggleEntryValue(section, "vb0"))] {
			if hash := toggleEntryValue(section, "hash"); hash != "" {
				return hash
			}
		}
	}
	return ""
}

func buildToggleViewerTXT(iniPath string, keys []toggleKeySection) string {
	lines := []string{"Mod: " + filepath.Base(filepath.Dir(iniPath)), "", "Ini: " + filepath.Base(iniPath), ""}
	for index, key := range keys {
		lines = append(lines, key.sectionName+":", "    Key: "+formatToggleKeySequence(key.keyValue))
		if key.backValue != "" {
			lines = append(lines, "    Back: "+formatToggleKeySequence(key.backValue))
		}
		if index < len(keys)-1 {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func buildToggleViewerINI(hash, hotkey string) string {
	return strings.Join([]string{
		"[Constants]", "global $active = 0", "global $enabled = 0", "",
		"[Key]", "key = " + hotkey, "condition = $active == 1", "type = cycle", "$enabled = 0,1", "",
		"[TextureOverrideCharacterPosition]", "hash = " + hash, "$active = 1", "",
		"[Present]", "post $active = 0", "run = CommandListKey", "",
		"[CommandListKey]", "if $active == 1 && $enabled == 1",
		"    pre Resource\\ShaderFixes\\help.ini\\NotificationParams = ResourceBox",
		"    pre run = CustomShader\\ShaderFixes\\help.ini\\FormatText",
		"    pre Resource\\ShaderFixes\\help.ini\\Notification = Resourcename1", "endif", "",
		"[ResourceBox]", "type = StructuredBuffer", "array = 1",
		"data = R32_FLOAT   -0.95 -1 1 1      1 1 1 1    0 0 0 0.95   0.05 0.05     1 2   0  1.0", "",
		"[Resourcename1]", "type = buffer", "format = R8_UINT", "filename = toggle-viewer.txt", "",
	}, "\n")
}

func replaceToggleViewerHotkey(content, hotkey string) string {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inKey := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := toggleSectionRE.FindStringSubmatch(trimmed); len(match) == 2 {
			inKey = strings.EqualFold(strings.TrimSpace(match[1]), "key")
			continue
		}
		if inKey {
			separator := strings.Index(trimmed, "=")
			if separator >= 0 && strings.EqualFold(strings.TrimSpace(trimmed[:separator]), "key") {
				lines[index] = "key = " + hotkey
				return strings.Join(lines, newline)
			}
		}
	}
	return content
}

func generateToggleViewerArtifact(iniPath, content, hotkey string) *toggleViewerArtifact {
	sections := parseToggleINI(content)
	keys := findToggleKeySections(sections)
	if len(keys) == 0 {
		return nil
	}
	hash := resolveTogglePositionHash(sections)
	if hash == "" {
		return nil
	}
	dir := filepath.Dir(iniPath)
	txt, ini := buildToggleViewerTXT(iniPath, keys), buildToggleViewerINI(hash, hotkey)
	return &toggleViewerArtifact{
		targetINIPath: iniPath, toggleTXTPath: filepath.Join(dir, "toggle-viewer.txt"),
		toggleINIPath: filepath.Join(dir, "toggle-viewer.ini"), toggleTXTHash: toggleContentSHA(txt),
		toggleINIHash: toggleContentSHA(ini), txtContent: txt, iniContent: ini,
	}
}

func toggleEntryValue(section toggleINISection, key string) string {
	for _, entry := range section.entries {
		if strings.EqualFold(entry.key, key) {
			return entry.value
		}
	}
	return ""
}

func toggleContentSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func formatToggleKeySequence(value string) string {
	if value == "" {
		return ""
	}
	var labels []string
	for _, key := range strings.Split(value, " ") {
		if label := formatToggleKeyLabel(key); label != "" {
			labels = append(labels, label)
		}
	}
	return strings.Join(labels, " + ")
}

func formatToggleKeyLabel(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || strings.HasPrefix(strings.ToLower(trimmed), "no_") {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	labels := map[string]string{
		"VK_CONTROL": "Ctrl", "CONTROL": "Ctrl", "VK_LCONTROL": "Ctrl", "VK_RCONTROL": "Ctrl",
		"VK_MENU": "Alt", "ALT": "Alt", "VK_LMENU": "Alt", "VK_RMENU": "Alt",
		"VK_SHIFT": "Shift", "SHIFT": "Shift", "VK_LSHIFT": "Shift", "VK_RSHIFT": "Shift",
		"VK_LWIN": "Win", "VK_RWIN": "Win", "VK_APPS": "Menu",
		"VK_UP": "Up", "UP": "Up", "VK_DOWN": "Down", "DOWN": "Down", "VK_LEFT": "Left", "LEFT": "Left", "VK_RIGHT": "Right", "RIGHT": "Right",
		"VK_HOME": "Home", "VK_END": "End", "VK_PRIOR": "PgUp", "VK_NEXT": "PgDn",
		"VK_RETURN": "Enter", "ENTER": "Enter", "VK_BACK": "Backspace", "VK_TAB": "Tab", "VK_SPACE": "Space",
		"VK_ESCAPE": "Esc", "VK_DELETE": "Del", "VK_INSERT": "Ins", "VK_SNAPSHOT": "PrtSc", "VK_PAUSE": "Pause",
		"VK_OEM_3": "`", "VK_OEM_MINUS": "-", "VK_OEM_PLUS": "+", "VK_OEM_COMMA": ",", "VK_OEM_PERIOD": ".",
		"VK_OEM_1": ";", "VK_OEM_2": "/", "VK_OEM_4": "[", "VK_OEM_5": "\\", "VK_OEM_6": "]", "VK_OEM_7": "'",
		"XB_LEFT_TRIGGER": "LT", "XB_RIGHT_TRIGGER": "RT", "XB_LEFT_SHOULDER": "LB", "XB_RIGHT_SHOULDER": "RB",
		"XB_LEFT_THUMB": "LS", "XB_RIGHT_THUMB": "RS", "XB_DPAD_UP": "D-Pad Up", "XB_DPAD_DOWN": "D-Pad Down",
		"XB_DPAD_LEFT": "D-Pad Left", "XB_DPAD_RIGHT": "D-Pad Right", "XB_A": "A", "XB_B": "B", "XB_X": "X", "XB_Y": "Y",
		"XB_START": "Start", "XB_BACK": "Back", "XB_GUIDE": "Guide",
	}
	if label := labels[upper]; label != "" {
		return label
	}
	if strings.HasPrefix(upper, "VK_") {
		stripped := strings.TrimPrefix(upper, "VK_")
		if len(stripped) == 1 || regexp.MustCompile(`^F\d+$`).MatchString(stripped) {
			return stripped
		}
		if strings.HasPrefix(stripped, "NUMPAD") {
			return strings.Replace(stripped, "NUMPAD", "Num", 1)
		}
	}
	if strings.HasPrefix(upper, "XB_") {
		return strings.TrimPrefix(upper, "XB_")
	}
	runes := []rune(trimmed)
	if len(runes) == 0 {
		return ""
	}
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}
