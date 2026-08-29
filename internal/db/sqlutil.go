package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func defaultSQLEqual(existing, target *string) bool {
	left := normalizeDefaultSQL(existing)
	right := normalizeDefaultSQL(target)
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func normalizeDefaultSQL(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	for strings.HasPrefix(normalized, "(") && strings.HasSuffix(normalized, ")") && len(normalized) > 1 {
		normalized = strings.TrimSpace(normalized[1 : len(normalized)-1])
	}
	if strings.EqualFold(normalized, "NULL") {
		n := "NULL"
		return &n
	}
	return &normalized
}

func normalizeType(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func sortStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func normalizeForeignKeySignature(fk ForeignKeySpec) string {
	onDelete := fk.OnDelete
	if onDelete == "" {
		onDelete = FKNoAction
	}
	onUpdate := fk.OnUpdate
	if onUpdate == "" {
		onUpdate = FKNoAction
	}
	raw, _ := json.Marshal(struct {
		Columns    []string `json:"columns"`
		OnDelete   string   `json:"onDelete"`
		OnUpdate   string   `json:"onUpdate"`
		RefColumns []string `json:"refColumns"`
		RefTable   string   `json:"refTable"`
	}{
		Columns:    fk.Columns,
		OnDelete:   string(onDelete),
		OnUpdate:   string(onUpdate),
		RefColumns: fk.RefColumns,
		RefTable:   fk.RefTable,
	})
	return string(raw)
}

func normalizeIndexSignature(index IndexSpec) string {
	raw, _ := json.Marshal(struct {
		Columns []string `json:"columns"`
		Name    string   `json:"name"`
		Unique  bool     `json:"unique"`
	}{
		Columns: index.Columns,
		Name:    index.Name,
		Unique:  index.Unique,
	})
	return string(raw)
}

func buildColumnDefinition(column ColumnSpec, compositePrimaryKey []string) string {
	parts := []string{quoteIdent(column.Name), string(column.Type)}
	if column.PrimaryKey && len(compositePrimaryKey) == 0 {
		parts = append(parts, "PRIMARY KEY")
	}
	if column.NotNull {
		parts = append(parts, "NOT NULL")
	}
	if column.DefaultSQL != nil {
		parts = append(parts, "DEFAULT "+*column.DefaultSQL)
	}
	return strings.Join(parts, " ")
}

func buildCreateTableSQL(spec TableSpec, tableName string) string {
	if tableName == "" {
		tableName = spec.Name
	}
	var composite []string
	if len(spec.CompositePrimaryKey) > 0 {
		composite = spec.CompositePrimaryKey
	}
	defs := make([]string, 0, len(spec.Columns)+1+len(spec.ForeignKeys))
	for _, column := range spec.Columns {
		defs = append(defs, buildColumnDefinition(column, composite))
	}
	if len(composite) > 0 {
		quoted := make([]string, len(composite))
		for i, name := range composite {
			quoted[i] = quoteIdent(name)
		}
		defs = append(defs, "PRIMARY KEY ("+strings.Join(quoted, ", ")+")")
	}
	for _, fk := range spec.ForeignKeys {
		onDelete := fk.OnDelete
		if onDelete == "" {
			onDelete = FKNoAction
		}
		onUpdate := fk.OnUpdate
		if onUpdate == "" {
			onUpdate = FKNoAction
		}
		cols := make([]string, len(fk.Columns))
		for i, name := range fk.Columns {
			cols[i] = quoteIdent(name)
		}
		refs := make([]string, len(fk.RefColumns))
		for i, name := range fk.RefColumns {
			refs[i] = quoteIdent(name)
		}
		defs = append(defs, fmt.Sprintf(
			"FOREIGN KEY (%s) REFERENCES %s (%s) ON DELETE %s ON UPDATE %s",
			strings.Join(cols, ", "),
			quoteIdent(fk.RefTable),
			strings.Join(refs, ", "),
			strings.ToUpper(string(onDelete)),
			strings.ToUpper(string(onUpdate)),
		))
	}
	return "CREATE TABLE " + quoteIdent(tableName) + " (" + strings.Join(defs, ", ") + ")"
}

func buildIndexSQL(tableName string, index IndexSpec) string {
	parts := []string{"CREATE"}
	if index.Unique {
		parts = append(parts, "UNIQUE")
	}
	cols := make([]string, len(index.Columns))
	for i, name := range index.Columns {
		cols[i] = quoteIdent(name)
	}
	parts = append(parts,
		"INDEX IF NOT EXISTS",
		quoteIdent(index.Name),
		"ON",
		quoteIdent(tableName),
		"("+strings.Join(cols, ", ")+")",
	)
	return strings.Join(parts, " ")
}

func ptrString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func ptrInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func argString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func argInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func toBool(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case int:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint64:
		return v != 0
	case float64:
		return v != 0
	case []byte:
		return toBool(string(v))
	case string:
		normalized := strings.TrimSpace(strings.ToLower(v))
		return normalized == "1" || normalized == "true"
	default:
		return false
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func toBytes(value any) []byte {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		out := make([]byte, len(v))
		copy(out, v)
		return out
	case string:
		return []byte(v)
	default:
		return []byte(fmt.Sprint(v))
	}
}
