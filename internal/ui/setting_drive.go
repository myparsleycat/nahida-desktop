package ui

import (
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/setting"
)

var driveSettingKeys = []string{
	setting.KeyDriveNameSortPolicy,
	setting.KeyDriveAutoTryPasswords,
	setting.KeyDrivePasswordList,
}

// drivePasswordListMax matches the limit the setting package keeps.
const drivePasswordListMax = 10

// passwordList edits the saved share-link passwords. They are shown in plain text on purpose: the user typed
// these low-sensitivity passwords and needs to review them.
type passwordList struct {
	rows   []passwordRow
	nextID int
	// saved is the list as last loaded or saved. A stored list that differs from it changed elsewhere and
	// replaces the rows; the echo of the screen's own save does not, so a row being typed in survives it.
	saved  []string
	loaded bool
}

type passwordRow struct {
	id     int
	value  string
	locked bool
}

func (s *Shell) driveSettings() *fairy.El {
	if !s.settingsLoaded(driveSettingKeys...) {
		return s.settingsLoading()
	}
	const prefix = "page.setting.drive."
	policies := []kit.Item{
		{Value: "natural_ignore_spacing", Label: T(prefix + "nameSortPolicy.options.natural_ignore_spacing")},
		{Value: "natural", Label: T(prefix + "nameSortPolicy.options.natural")},
	}
	return settingsBody(
		settingsGroup(T(prefix+"sorting.title"),
			settingRow(T(prefix+"nameSortPolicy.title"), T(prefix+"nameSortPolicy.description"),
				s.settingSelect(setting.KeyDriveNameSortPolicy, policies, 208,
					kit.Placeholder(T(prefix+"nameSortPolicy.select")), kit.Label(T(prefix+"nameSortPolicy.title")))),
		),
		settingsGroup(T(prefix+"import.title"),
			s.settingSwitch(setting.KeyDriveAutoTryPasswords, T(prefix+"autoTryPasswords.title"),
				T(prefix+"autoTryPasswords.description")),
			s.passwordListSetting(),
		),
	)
}

func (s *Shell) passwordListSetting() *fairy.El {
	const prefix = "page.setting.drive.passwordList."
	t := kit.T()
	list := &s.settings.passwords
	stored := normalizePasswords(s.stringsSetting(setting.KeyDrivePasswordList))
	if !list.loaded || !slices.Equal(stored, list.saved) {
		list.rows = list.rows[:0]
		for _, value := range stored {
			list.add(value, true)
		}
		list.saved, list.loaded = stored, true
	}

	save := func() {
		values := make([]string, 0, len(list.rows))
		for _, row := range list.rows {
			values = append(values, row.value)
		}
		list.saved = normalizePasswords(values)
		s.setSetting(setting.KeyDrivePasswordList, values)
	}
	remove := func(id int) {
		list.rows = slices.DeleteFunc(list.rows, func(row passwordRow) bool { return row.id == id })
		save()
	}

	rows := fairy.Col().Gap(6)
	for i := range list.rows {
		row := &list.rows[i]
		id := row.id
		removeButton := kit.IconButton(lucide.X, func() { remove(id) }, kit.Ghost, kit.Small, kit.Label(T("g.delete"))).
			Size(28, 28).Shrink(0)
		if row.locked {
			rows.Child(fairy.Row(
				fairy.Box(fairy.Text(row.value).FontSize(14).LineHeight(20).Ellipsis()).H(32).PadXY(12, 0).
					Grow(1).MinW(0).Justify(fairy.JustifyCenter),
				removeButton,
			).Key("password." + itoa(id)).Gap(6).Align(fairy.Center))
			continue
		}
		finish := func() {
			for i := range list.rows {
				if list.rows[i].id == id && strings.TrimSpace(list.rows[i].value) != "" {
					list.rows[i].locked = true
				}
			}
			save()
		}
		field := kit.TextField(row.value, func(value string) {
			for i := range list.rows {
				if list.rows[i].id == id {
					list.rows[i].value = value
				}
			}
		}, kit.Placeholder(T(prefix+"placeholder")), kit.Label(T(prefix+"title"))).
			Key("password.field." + itoa(id)).OnBlur(finish).OnSubmit(func(string) { finish() })
		rows.Child(fairy.Row(field, removeButton).Key("password." + itoa(id)).Gap(6).Align(fairy.Center))
	}
	rows.Child(kit.Button(T(prefix+"add"), func() {
		if len(list.rows) < drivePasswordListMax {
			list.add("", false)
		}
	}, kit.Outline, kit.Small, kit.Icon(lucide.Plus), kit.Disabled(len(list.rows) >= drivePasswordListMax)).
		AlignSelf(fairy.Start))

	return fairy.Col(
		fairy.Col(
			fairy.Text(T(prefix+"title")).FontSize(14).LineHeight(20).Medium(),
			fairy.Text(T(prefix+"description", "max", drivePasswordListMax)).FontSize(12).LineHeight(16).
				Color(t.MutedForeground),
		).Gap(2),
		rows,
	).Gap(8).PadXY(0, 12)
}

func (l *passwordList) add(value string, locked bool) {
	l.nextID++
	l.rows = append(l.rows, passwordRow{id: l.nextID, value: value, locked: locked})
}

// normalizePasswords is what the setting package stores for values: trimmed, without empty entries, at most
// drivePasswordListMax.
func normalizePasswords(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && len(out) < drivePasswordListMax {
			out = append(out, value)
		}
	}
	return out
}
