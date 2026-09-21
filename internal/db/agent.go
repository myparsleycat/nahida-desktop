package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type AgentSessionsStore struct{ c *Client }

type AgentEventsStore struct{ c *Client }

type AgentMCPServersStore struct{ c *Client }

func scanAgentSession(scanner interface{ Scan(...any) error }) (*AgentSessionRow, error) {
	var row AgentSessionRow
	var modPath, modName sql.NullString
	err := scanner.Scan(
		&row.ID, &row.ScopeType, &modPath, &modName, &row.Title, &row.DurableSummary, &row.Revert, &row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	row.ModPath = ptrString(modPath)
	row.ModName = ptrString(modName)
	return &row, nil
}

const agentSessionSelect = `SELECT "id", "scope_type", "mod_path", "mod_name", "title",
"durable_summary", "revert", "created_at", "updated_at" FROM "agent_session"`

func (s AgentSessionsStore) Insert(ctx context.Context, row AgentSessionRow) error {
	return s.c.exec(ctx, `INSERT INTO "agent_session"
("id", "scope_type", "mod_path", "mod_name", "title", "durable_summary", "created_at", "updated_at")
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, row.ID, row.ScopeType, argString(row.ModPath), argString(row.ModName), row.Title,
		row.DurableSummary, row.CreatedAt, row.UpdatedAt)
}

func (s AgentSessionsStore) Get(ctx context.Context, id string) (*AgentSessionRow, error) {
	row, err := scanAgentSession(
		s.c.db.QueryRowContext(ctx, agentSessionSelect+` WHERE "id" = ?`, id),
	)
	if isNoRows(err) {
		return nil, nil
	}
	return row, err
}

func (s AgentSessionsStore) List(ctx context.Context) ([]AgentSessionRow, error) {
	rows, err := s.c.query(ctx, agentSessionSelect+` ORDER BY "updated_at" DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]AgentSessionRow, 0)
	for rows.Next() {
		row, err := scanAgentSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func (s AgentSessionsStore) FindLatestScope(ctx context.Context, scopeType, modPath string) (*AgentSessionRow, error) {
	query := agentSessionSelect + ` WHERE "scope_type" = ? AND COALESCE("mod_path", '') = ? ORDER BY "updated_at" DESC LIMIT 1`
	row, err := scanAgentSession(s.c.db.QueryRowContext(ctx, query, scopeType, modPath))
	if isNoRows(err) {
		return nil, nil
	}
	return row, err
}

func (s AgentSessionsStore) Rename(ctx context.Context, id, title, updatedAt string) error {
	return s.c.exec(
		ctx,
		`UPDATE "agent_session" SET "title" = ?, "updated_at" = ? WHERE "id" = ?`,
		title,
		updatedAt,
		id,
	)
}

// RenameIfUnchanged replaces a title only while the stored one still matches, so an automatic
// rename never overwrites a title the user set in the meantime.
func (s AgentSessionsStore) RenameIfUnchanged(ctx context.Context, id, current, title, updatedAt string) (bool, error) {
	result, err := s.c.db.ExecContext(
		ctx,
		`UPDATE "agent_session" SET "title" = ?, "updated_at" = ? WHERE "id" = ? AND "title" = ?`,
		title,
		updatedAt,
		id,
		current,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s AgentSessionsStore) Touch(ctx context.Context, id, updatedAt string) error {
	return s.c.exec(ctx, `UPDATE "agent_session" SET "updated_at" = ? WHERE "id" = ?`, updatedAt, id)
}

func (s AgentSessionsStore) UpdateSummary(ctx context.Context, id, summary, updatedAt string) error {
	return s.c.exec(ctx, `UPDATE "agent_session" SET "durable_summary" = ?, "updated_at" = ? WHERE "id" = ?`,
		summary, updatedAt, id)
}

// SetRevert stages a revert on the session. The row is untouched until the next Send commits it.
func (s AgentSessionsStore) SetRevert(ctx context.Context, id, revert, updatedAt string) error {
	return s.c.exec(
		ctx,
		`UPDATE "agent_session" SET "revert" = ?, "updated_at" = ? WHERE "id" = ?`,
		revert,
		updatedAt,
		id,
	)
}

// ClearRevert drops a staged revert without deleting anything.
func (s AgentSessionsStore) ClearRevert(ctx context.Context, id, updatedAt string) error {
	return s.c.exec(ctx, `UPDATE "agent_session" SET "revert" = '', "updated_at" = ? WHERE "id" = ?`, updatedAt, id)
}

// CommitRevertAndAppend applies a staged revert and appends the new turn/start event in one
// transaction. The marker is claimed before the reverted events are removed, so a concurrent send
// cannot delete the new event accepted by an earlier commit.
func (s AgentSessionsStore) CommitRevertAndAppend(
	ctx context.Context,
	id string,
	boundarySequence int64,
	revert, summary string,
	event AgentEventRow,
) (int64, error) {
	var sequence int64
	err := s.c.withImmediate(ctx, func(tx queryExec) error {
		claim, err := tx.ExecContext(ctx, `UPDATE "agent_session" SET "durable_summary" = ?, "revert" = '',
"updated_at" = ? WHERE "id" = ? AND "revert" = ?`, summary, event.CreatedAt, id, revert)
		if err != nil {
			return err
		}
		affected, err := claim.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return errors.New("agent revert changed concurrently")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM "agent_approval" WHERE "session_id" = ? AND "turn_id" IN (
SELECT DISTINCT "turn_id" FROM "agent_event" WHERE "session_id" = ? AND "sequence" >= ?)`,
			id, id, boundarySequence); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM "agent_event" WHERE "session_id" = ? AND "sequence" >= ?`, id, boundarySequence,
		); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX("sequence"), 0) + 1 FROM "agent_event" WHERE "session_id" = ?`, id,
		).Scan(&sequence); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO "agent_event"
("session_id", "sequence", "turn_id", "event_type", "payload", "created_at") VALUES (?, ?, ?, ?, ?, ?)`,
			event.SessionID, sequence, event.TurnID, event.EventType, event.Payload, event.CreatedAt,
		)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("commit agent revert and append event: %w", err)
	}
	return sequence, nil
}

func (s AgentSessionsStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "agent_session" WHERE "id" = ?`, id)
}

func (s AgentEventsStore) Append(ctx context.Context, row AgentEventRow) (int64, error) {
	var sequence int64
	err := s.c.withImmediate(ctx, func(tx queryExec) error {
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX("sequence"), 0) + 1 FROM "agent_event" WHERE "session_id" = ?`, row.SessionID,
		).Scan(&sequence); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO "agent_event"
("session_id", "sequence", "turn_id", "event_type", "payload", "created_at") VALUES (?, ?, ?, ?, ?, ?)`,
			row.SessionID, sequence, row.TurnID, row.EventType, row.Payload, row.CreatedAt)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("append agent event: %w", err)
	}
	return sequence, nil
}

func (s AgentEventsStore) AppendAndUpdateSummary(
	ctx context.Context,
	row AgentEventRow,
	summary string,
) (int64, error) {
	var sequence int64
	err := s.c.withImmediate(ctx, func(tx queryExec) error {
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX("sequence"), 0) + 1 FROM "agent_event" WHERE "session_id" = ?`, row.SessionID,
		).Scan(&sequence); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "agent_event"
("session_id", "sequence", "turn_id", "event_type", "payload", "created_at") VALUES (?, ?, ?, ?, ?, ?)`,
			row.SessionID, sequence, row.TurnID, row.EventType, row.Payload, row.CreatedAt); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx,
			`UPDATE "agent_session" SET "durable_summary" = ?, "updated_at" = ? WHERE "id" = ?`,
			summary, row.CreatedAt, row.SessionID,
		)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return fmt.Errorf("update agent summary: session %q not found", row.SessionID)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("append agent summary: %w", err)
	}
	return sequence, nil
}

func (s AgentEventsStore) List(ctx context.Context, sessionID string) ([]AgentEventRow, error) {
	rows, err := s.c.query(ctx, `SELECT "session_id", "sequence", "turn_id", "event_type", "payload", "created_at"
FROM "agent_event" WHERE "session_id" = ? ORDER BY "sequence"`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]AgentEventRow, 0)
	for rows.Next() {
		var row AgentEventRow
		if err := rows.Scan(
			&row.SessionID,
			&row.Sequence,
			&row.TurnID,
			&row.EventType,
			&row.Payload,
			&row.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// LatestSummaryBefore returns the payload of the latest compaction summary event before sequence, or
// an empty string when the surviving events carry none.
func (s AgentEventsStore) LatestSummaryBefore(ctx context.Context, sessionID string, sequence int64) (string, error) {
	var payload string
	err := s.c.db.QueryRowContext(ctx, `SELECT "payload" FROM "agent_event"
WHERE "session_id" = ? AND "event_type" = 'summary' AND "sequence" < ? ORDER BY "sequence" DESC LIMIT 1`,
		sessionID, sequence).Scan(&payload)
	if isNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return payload, nil
}

func (s AgentEventsStore) SealInterrupted(ctx context.Context, createdAt string) error {
	type interruptedTurn struct{ sessionID, turnID string }
	turns, err := func() ([]interruptedTurn, error) {
		rows, err := s.c.query(ctx, `SELECT e."session_id", e."turn_id" FROM "agent_event" e
WHERE e."event_type" = 'turn/start' AND NOT EXISTS (
  SELECT 1 FROM "agent_event" terminal WHERE terminal."session_id" = e."session_id" AND terminal."turn_id" = e."turn_id"
  AND terminal."event_type" IN ('turn/end', 'turn/error', 'turn/cancelled', 'turn/interrupted', 'turn/awaiting-approval')
) GROUP BY e."session_id", e."turn_id"`)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		result := make([]interruptedTurn, 0)
		for rows.Next() {
			var turn interruptedTurn
			if err := rows.Scan(&turn.sessionID, &turn.turnID); err != nil {
				return nil, err
			}
			result = append(result, turn)
		}
		return result, rows.Err()
	}()
	if err != nil {
		return err
	}
	for _, turn := range turns {
		if _, err := s.Append(ctx, AgentEventRow{
			SessionID: turn.sessionID,
			TurnID:    turn.turnID,
			EventType: "turn/interrupted",
			Payload:   `{"reason":"application-restarted"}`,
			CreatedAt: createdAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func scanAgentMCPServer(scanner interface{ Scan(...any) error }) (*AgentMCPServerRow, error) {
	var row AgentMCPServerRow
	err := scanner.Scan(&row.ID, &row.Name, &row.Transport, &row.PublicConfig, &row.SecretBlob, &row.Enabled,
		&row.CreatedAt, &row.UpdatedAt)
	return &row, err
}

func (s AgentMCPServersStore) List(ctx context.Context) ([]AgentMCPServerRow, error) {
	rows, err := s.c.query(ctx, `SELECT "id", "name", "transport", "public_config", "secret_blob", "enabled",
"created_at", "updated_at" FROM "agent_mcp_server" ORDER BY "name"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]AgentMCPServerRow, 0)
	for rows.Next() {
		row, err := scanAgentMCPServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func (s AgentMCPServersStore) Get(ctx context.Context, id string) (*AgentMCPServerRow, error) {
	row, err := scanAgentMCPServer(s.c.db.QueryRowContext(ctx, `SELECT "id", "name", "transport", "public_config",
"secret_blob", "enabled", "created_at", "updated_at" FROM "agent_mcp_server" WHERE "id" = ?`, id))
	if isNoRows(err) {
		return nil, nil
	}
	return row, err
}

func (s AgentMCPServersStore) Upsert(ctx context.Context, row AgentMCPServerRow) error {
	return s.c.exec(ctx, `INSERT INTO "agent_mcp_server"
("id", "name", "transport", "public_config", "secret_blob", "enabled", "created_at", "updated_at")
VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT("id") DO UPDATE SET "name"=excluded."name", "transport"=excluded."transport",
"public_config"=excluded."public_config", "secret_blob"=excluded."secret_blob", "enabled"=excluded."enabled",
"updated_at"=excluded."updated_at"`, row.ID, row.Name, row.Transport, row.PublicConfig, row.SecretBlob, row.Enabled,
		row.CreatedAt, row.UpdatedAt)
}

func (s AgentMCPServersStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "agent_mcp_server" WHERE "id" = ?`, id)
}
