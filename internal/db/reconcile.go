package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type tableInfoRow struct {
	CID       int
	Name      string
	Type      string
	NotNull   int
	DfltValue sql.NullString
	PK        int
}

type foreignKeyRow struct {
	ID       int
	Seq      int
	Table    string
	From     string
	To       string
	OnUpdate string
	OnDelete string
	Match    string
}

type indexListRow struct {
	Seq     int
	Name    string
	Unique  int
	Origin  string
	Partial int
}

type indexInfoRow struct {
	SeqNo int
	CID   int
	Name  string
}

type existingTableShape struct {
	tableName   string
	columns     []tableInfoRow
	foreignKeys []ForeignKeySpec
	indexes     []IndexSpec
}

type reconcileCandidate struct {
	spec       TableSpec
	actualName string
	shape      *existingTableShape
}

type reconcileAction struct {
	kind    string
	columns []ColumnSpec
}

// Reconcile creates missing tables, ADD COLUMNs when safe, rebuilds on
// name/type/null/PK/FK/extra-column mismatch, ensures indexes, records
// app_schema_version, and applies one-shot migrations.
func (c *Client) Reconcile(ctx context.Context) error {
	tableNames, err := c.listUserTables(ctx)
	if err != nil {
		return err
	}

	candidates := make([]reconcileCandidate, 0, len(TableSpecs))
	for _, spec := range TableSpecs {
		candidate, err := c.buildReconcileCandidate(ctx, spec, tableNames)
		if err != nil {
			return err
		}
		candidates = append(candidates, candidate)
	}

	if err := c.exec(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign_keys: %w", err)
	}

	restore := func() error {
		return c.exec(ctx, `PRAGMA foreign_keys = ON`)
	}

	for _, candidate := range candidates {
		if err := c.reconcileTable(ctx, candidate); err != nil {
			_ = restore()
			return err
		}
	}

	if err := restore(); err != nil {
		return err
	}

	if err := c.SchemaState.Upsert(ctx, SchemaKeyAppVersion, fmt.Sprintf("%d", AppSchemaVersion), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}

	if err := c.migrateGamePathsNteLauncherPath(ctx); err != nil {
		return err
	}
	return c.dropToggleViewerArtifactTable(ctx)
}

func (c *Client) listUserTables(ctx context.Context) (map[string]struct{}, error) {
	rows, err := c.query(ctx, `SELECT "name" FROM "sqlite_schema" WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	names := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = struct{}{}
	}
	return names, rows.Err()
}

func (c *Client) buildReconcileCandidate(ctx context.Context, spec TableSpec, tableNames map[string]struct{}) (reconcileCandidate, error) {
	candidates := append([]string{spec.Name}, spec.Aliases...)
	var actual string
	for _, name := range candidates {
		if _, ok := tableNames[name]; ok {
			actual = name
			break
		}
	}
	if actual == "" {
		return reconcileCandidate{spec: spec}, nil
	}
	shape, err := c.readTableShape(ctx, actual)
	if err != nil {
		return reconcileCandidate{}, err
	}
	return reconcileCandidate{spec: spec, actualName: actual, shape: shape}, nil
}

func (c *Client) readTableShape(ctx context.Context, tableName string) (*existingTableShape, error) {
	columns, err := c.pragmaTableInfo(ctx, tableName)
	if err != nil {
		return nil, err
	}
	fkRows, err := c.pragmaForeignKeyList(ctx, tableName)
	if err != nil {
		return nil, err
	}
	indexList, err := c.pragmaIndexList(ctx, tableName)
	if err != nil {
		return nil, err
	}

	indexes := make([]IndexSpec, 0, len(indexList))
	for _, index := range indexList {
		if index.Origin == "pk" {
			continue
		}
		info, err := c.pragmaIndexInfo(ctx, index.Name)
		if err != nil {
			return nil, err
		}
		cols := make([]string, len(info))
		for i, row := range info {
			cols[i] = row.Name
		}
		indexes = append(indexes, IndexSpec{
			Name:    index.Name,
			Columns: cols,
			Unique:  index.Unique != 0,
		})
	}

	return &existingTableShape{
		tableName:   tableName,
		columns:     columns,
		foreignKeys: groupForeignKeys(fkRows),
		indexes:     indexes,
	}, nil
}

func (c *Client) pragmaTableInfo(ctx context.Context, tableName string) ([]tableInfoRow, error) {
	rows, err := c.query(ctx, `PRAGMA table_info(`+quoteIdent(tableName)+`)`)
	if err != nil {
		return nil, fmt.Errorf("table_info %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	var out []tableInfoRow
	for rows.Next() {
		var row tableInfoRow
		if err := rows.Scan(&row.CID, &row.Name, &row.Type, &row.NotNull, &row.DfltValue, &row.PK); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (c *Client) pragmaForeignKeyList(ctx context.Context, tableName string) ([]foreignKeyRow, error) {
	rows, err := c.query(ctx, `PRAGMA foreign_key_list(`+quoteIdent(tableName)+`)`)
	if err != nil {
		return nil, fmt.Errorf("foreign_key_list %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	var out []foreignKeyRow
	for rows.Next() {
		var row foreignKeyRow
		if err := rows.Scan(&row.ID, &row.Seq, &row.Table, &row.From, &row.To, &row.OnUpdate, &row.OnDelete, &row.Match); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (c *Client) pragmaIndexList(ctx context.Context, tableName string) ([]indexListRow, error) {
	rows, err := c.query(ctx, `PRAGMA index_list(`+quoteIdent(tableName)+`)`)
	if err != nil {
		return nil, fmt.Errorf("index_list %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	var out []indexListRow
	for rows.Next() {
		var row indexListRow
		if err := rows.Scan(&row.Seq, &row.Name, &row.Unique, &row.Origin, &row.Partial); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (c *Client) pragmaIndexInfo(ctx context.Context, indexName string) ([]indexInfoRow, error) {
	rows, err := c.query(ctx, `PRAGMA index_info(`+quoteIdent(indexName)+`)`)
	if err != nil {
		return nil, fmt.Errorf("index_info %s: %w", indexName, err)
	}
	defer func() { _ = rows.Close() }()

	var out []indexInfoRow
	for rows.Next() {
		var row indexInfoRow
		if err := rows.Scan(&row.SeqNo, &row.CID, &row.Name); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	// seqno order is already sequential; keep it.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].SeqNo < out[j-1].SeqNo; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, rows.Err()
}

func groupForeignKeys(rows []foreignKeyRow) []ForeignKeySpec {
	order := make([]int, 0)
	grouped := make(map[int]*ForeignKeySpec)
	for _, row := range rows {
		current, ok := grouped[row.ID]
		if !ok {
			current = &ForeignKeySpec{
				RefTable: row.Table,
				OnDelete: ForeignKeyAction(strings.ToLower(row.OnDelete)),
				OnUpdate: ForeignKeyAction(strings.ToLower(row.OnUpdate)),
			}
			grouped[row.ID] = current
			order = append(order, row.ID)
		}
		current.Columns = append(current.Columns, row.From)
		current.RefColumns = append(current.RefColumns, row.To)
	}
	out := make([]ForeignKeySpec, 0, len(order))
	for _, id := range order {
		out = append(out, *grouped[id])
	}
	return out
}

func (c *Client) reconcileTable(ctx context.Context, candidate reconcileCandidate) error {
	if candidate.actualName == "" || candidate.shape == nil {
		if err := c.exec(ctx, buildCreateTableSQL(candidate.spec, candidate.spec.Name)); err != nil {
			return fmt.Errorf("create table %s: %w", candidate.spec.Name, err)
		}
		return c.ensureIndexes(ctx, candidate.spec)
	}

	action := getReconcileAction(candidate.spec, candidate.shape)
	switch action.kind {
	case "noop":
		return c.ensureIndexes(ctx, candidate.spec)
	case "add-columns":
		for _, column := range action.columns {
			sql := `ALTER TABLE ` + quoteIdent(candidate.shape.tableName) + ` ADD COLUMN ` + buildColumnDefinition(column, nil)
			if err := c.exec(ctx, sql); err != nil {
				return fmt.Errorf("add column %s.%s: %w", candidate.shape.tableName, column.Name, err)
			}
		}
		return c.ensureIndexes(ctx, candidate.spec)
	default:
		return c.rebuildTable(ctx, candidate.spec, candidate.shape)
	}
}

func getReconcileAction(spec TableSpec, shape *existingTableShape) reconcileAction {
	if shape.tableName != spec.Name {
		return reconcileAction{kind: "rebuild"}
	}

	existingByName := make(map[string]tableInfoRow, len(shape.columns))
	for _, column := range shape.columns {
		existingByName[column.Name] = column
	}

	for _, column := range shape.columns {
		found := false
		for _, target := range spec.Columns {
			if target.Name == column.Name {
				found = true
				break
			}
		}
		if !found {
			return reconcileAction{kind: "rebuild"}
		}
	}

	var missing []ColumnSpec
	for _, target := range spec.Columns {
		existing, ok := existingByName[target.Name]
		if !ok {
			for _, alias := range target.Aliases {
				if _, has := existingByName[alias]; has {
					return reconcileAction{kind: "rebuild"}
				}
			}
			if target.NotNull && target.DefaultSQL == nil {
				return reconcileAction{kind: "rebuild"}
			}
			missing = append(missing, target)
			continue
		}

		if normalizeType(existing.Type) != normalizeType(string(target.Type)) {
			return reconcileAction{kind: "rebuild"}
		}
		if (existing.NotNull != 0) != target.NotNull {
			return reconcileAction{kind: "rebuild"}
		}
		if len(spec.CompositePrimaryKey) == 0 && target.PrimaryKey != (existing.PK > 0) {
			return reconcileAction{kind: "rebuild"}
		}

		var existingDefault *string
		if existing.DfltValue.Valid {
			v := existing.DfltValue.String
			existingDefault = &v
		}
		if !defaultSQLEqual(existingDefault, target.DefaultSQL) {
			return reconcileAction{kind: "rebuild"}
		}
	}

	if len(spec.CompositePrimaryKey) > 0 {
		var existingPK []string
		pkCols := append([]tableInfoRow(nil), shape.columns...)
		for i := 1; i < len(pkCols); i++ {
			for j := i; j > 0 && pkCols[j].PK < pkCols[j-1].PK; j-- {
				pkCols[j], pkCols[j-1] = pkCols[j-1], pkCols[j]
			}
		}
		for _, column := range pkCols {
			if column.PK > 0 {
				existingPK = append(existingPK, column.Name)
			}
		}
		if strings.Join(sortStrings(existingPK), "\x00") != strings.Join(sortStrings(spec.CompositePrimaryKey), "\x00") {
			return reconcileAction{kind: "rebuild"}
		}
	}

	existingFKs := make(map[string]struct{}, len(shape.foreignKeys))
	for _, fk := range shape.foreignKeys {
		existingFKs[normalizeForeignKeySignature(fk)] = struct{}{}
	}
	targetFKs := make(map[string]struct{}, len(spec.ForeignKeys))
	for _, fk := range spec.ForeignKeys {
		targetFKs[normalizeForeignKeySignature(fk)] = struct{}{}
	}
	if len(existingFKs) != len(targetFKs) {
		return reconcileAction{kind: "rebuild"}
	}
	for sig := range targetFKs {
		if _, ok := existingFKs[sig]; !ok {
			return reconcileAction{kind: "rebuild"}
		}
	}

	if len(missing) > 0 {
		return reconcileAction{kind: "add-columns", columns: missing}
	}
	return reconcileAction{kind: "noop"}
}

func (c *Client) rebuildTable(ctx context.Context, spec TableSpec, shape *existingTableShape) error {
	tempTableName := "__new_" + spec.Name
	sourceColumns := make(map[string]struct{}, len(shape.columns))
	for _, column := range shape.columns {
		sourceColumns[column.Name] = struct{}{}
	}

	insertColumns := make([]string, 0, len(spec.Columns))
	selectExpressions := make([]string, 0, len(spec.Columns))
	for _, target := range spec.Columns {
		sourceName, err := findSourceColumnName(target, sourceColumns)
		if err != nil {
			return err
		}
		insertColumns = append(insertColumns, quoteIdent(target.Name))
		selectExpressions = append(selectExpressions, buildCopyExpression(target, sourceName)+" AS "+quoteIdent(target.Name))
	}

	return c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(ctx, buildCreateTableSQL(spec, tempTableName)); err != nil {
			return fmt.Errorf("create temp %s: %w", spec.Name, err)
		}
		copySQL := fmt.Sprintf(
			`INSERT INTO %s (%s) SELECT %s FROM %s`,
			quoteIdent(tempTableName),
			strings.Join(insertColumns, ", "),
			strings.Join(selectExpressions, ", "),
			quoteIdent(shape.tableName),
		)
		if _, err := tx.ExecContext(ctx, copySQL); err != nil {
			return fmt.Errorf("copy %s: %w", spec.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+quoteIdent(shape.tableName)); err != nil {
			return fmt.Errorf("drop old %s: %w", shape.tableName, err)
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+quoteIdent(tempTableName)+` RENAME TO `+quoteIdent(spec.Name)); err != nil {
			return fmt.Errorf("rename %s: %w", spec.Name, err)
		}
		return c.ensureIndexesOn(ctx, tx, spec)
	})
}

func findSourceColumnName(target ColumnSpec, sourceColumns map[string]struct{}) (string, error) {
	if _, ok := sourceColumns[target.Name]; ok {
		return target.Name, nil
	}
	var matches []string
	for _, alias := range target.Aliases {
		if _, ok := sourceColumns[alias]; ok {
			matches = append(matches, alias)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous column aliases for %s", target.Name)
	}
	return "", nil
}

func buildCopyExpression(target ColumnSpec, sourceName string) string {
	if sourceName == "" {
		if target.DefaultSQL != nil {
			return *target.DefaultSQL
		}
		return "NULL"
	}
	sourceSQL := quoteIdent(sourceName)
	if !target.Boolean {
		return sourceSQL
	}
	fallback := "0"
	if target.DefaultSQL != nil {
		fallback = *target.DefaultSQL
	}
	return fmt.Sprintf(`CASE
            WHEN %s IS NULL THEN %s
            WHEN LOWER(CAST(%s AS TEXT)) IN ('1', 'true') THEN 1
            ELSE 0
        END`, sourceSQL, fallback, sourceSQL)
}

func (c *Client) ensureIndexes(ctx context.Context, spec TableSpec) error {
	return c.ensureIndexesOn(ctx, c.db, spec)
}

func (c *Client) ensureIndexesOn(ctx context.Context, execer queryExec, spec TableSpec) error {
	if len(spec.Indexes) == 0 {
		return nil
	}
	indexList, err := pragmaIndexListOn(ctx, execer, spec.Name)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(indexList))
	for _, index := range indexList {
		info, err := pragmaIndexInfoOn(ctx, execer, index.Name)
		if err != nil {
			return err
		}
		cols := make([]string, len(info))
		for i, row := range info {
			cols[i] = row.Name
		}
		existing[normalizeIndexSignature(IndexSpec{
			Name:    index.Name,
			Columns: cols,
			Unique:  index.Unique != 0,
		})] = struct{}{}
	}
	for _, index := range spec.Indexes {
		if _, ok := existing[normalizeIndexSignature(index)]; ok {
			continue
		}
		if _, err := execer.ExecContext(ctx, buildIndexSQL(spec.Name, index)); err != nil {
			return fmt.Errorf("create index %s: %w", index.Name, err)
		}
	}
	return nil
}

func pragmaIndexListOn(ctx context.Context, execer queryExec, tableName string) ([]indexListRow, error) {
	rows, err := execer.QueryContext(ctx, `PRAGMA index_list(`+quoteIdent(tableName)+`)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []indexListRow
	for rows.Next() {
		var row indexListRow
		if err := rows.Scan(&row.Seq, &row.Name, &row.Unique, &row.Origin, &row.Partial); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func pragmaIndexInfoOn(ctx context.Context, execer queryExec, indexName string) ([]indexInfoRow, error) {
	rows, err := execer.QueryContext(ctx, `PRAGMA index_info(`+quoteIdent(indexName)+`)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []indexInfoRow
	for rows.Next() {
		var row indexInfoRow
		if err := rows.Scan(&row.SeqNo, &row.CID, &row.Name); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].SeqNo < out[j-1].SeqNo; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, rows.Err()
}

func (c *Client) migrateGamePathsNteLauncherPath(ctx context.Context) error {
	migrated, err := c.SchemaState.Get(ctx, SchemaKeyGamePathsNTELauncher)
	if err != nil {
		return err
	}
	if migrated != nil && migrated.Value == "1" {
		return nil
	}

	if err := c.exec(ctx, `
UPDATE "game_paths"
SET "nteLauncherPath" = "gameExecutablePath",
    "gameExecutablePath" = NULL
WHERE "importer" = ?
  AND "gameExecutablePath" IS NOT NULL
  AND lower("gameExecutablePath") NOT LIKE ?`,
		NTEImporter, NTEGameExeKeepSuffix); err != nil {
		return fmt.Errorf("migrate nte launcher path: %w", err)
	}

	return c.SchemaState.Upsert(ctx, SchemaKeyGamePathsNTELauncher, "1", time.Now().UTC().Format(time.RFC3339Nano))
}

func (c *Client) dropToggleViewerArtifactTable(ctx context.Context) error {
	migrated, err := c.SchemaState.Get(ctx, SchemaKeyToggleViewerArtifactDropped)
	if err != nil {
		return err
	}
	if migrated != nil && migrated.Value == "1" {
		return nil
	}

	if err := c.exec(ctx, `DROP TABLE IF EXISTS `+quoteIdent("toggle_viewer_artifact")); err != nil {
		return fmt.Errorf("drop toggle_viewer_artifact: %w", err)
	}
	if err := c.exec(ctx, `DELETE FROM "setting" WHERE "key" IN (?, ?)`,
		"xxmi_toggle_viewer_auto_generate", "xxmi_toggle_viewer_hotkey"); err != nil {
		return fmt.Errorf("delete toggle viewer settings: %w", err)
	}

	return c.SchemaState.Upsert(ctx, SchemaKeyToggleViewerArtifactDropped, "1", time.Now().UTC().Format(time.RFC3339Nano))
}
