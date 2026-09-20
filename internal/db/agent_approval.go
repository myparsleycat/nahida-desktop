package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type AgentApprovalsStore struct{ c *Client }

func scanAgentApproval(scanner interface{ Scan(...any) error }) (*AgentApprovalRow, error) {
	var row AgentApprovalRow
	var decidedAt, completedAt sql.NullString
	err := scanner.Scan(
		&row.ID,
		&row.SessionID,
		&row.TurnID,
		&row.ToolCallID,
		&row.ActionID,
		&row.Arguments,
		&row.Summary,
		&row.Target,
		&row.Impact,
		&row.Status,
		&row.Result,
		&row.Error,
		&row.CreatedAt,
		&decidedAt,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}
	row.DecidedAt = ptrString(decidedAt)
	row.CompletedAt = ptrString(completedAt)
	return &row, nil
}

func (s AgentApprovalsStore) Insert(ctx context.Context, row AgentApprovalRow) error {
	return s.c.exec(ctx, `INSERT INTO "agent_approval"
("id", "session_id", "turn_id", "tool_call_id", "action_id", "arguments", "summary", "target", "impact",
 "status", "result", "error", "created_at", "decided_at", "completed_at")
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID,
		row.SessionID,
		row.TurnID,
		row.ToolCallID,
		row.ActionID,
		row.Arguments,
		row.Summary,
		row.Target,
		row.Impact,
		row.Status,
		row.Result,
		row.Error,
		row.CreatedAt,
		argString(row.DecidedAt),
		argString(row.CompletedAt),
	)
}

func (s AgentApprovalsStore) Get(ctx context.Context, id string) (*AgentApprovalRow, error) {
	row, err := scanAgentApproval(s.c.db.QueryRowContext(ctx, agentApprovalSelect+` WHERE "id" = ?`, id))
	if isNoRows(err) {
		return nil, nil
	}
	return row, err
}

func (s AgentApprovalsStore) ListSession(ctx context.Context, sessionID string) ([]AgentApprovalRow, error) {
	rows, err := s.c.query(ctx, agentApprovalSelect+` WHERE "session_id" = ? ORDER BY "created_at"`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]AgentApprovalRow, 0)
	for rows.Next() {
		row, err := scanAgentApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *row)
	}
	return result, rows.Err()
}

func (s AgentApprovalsStore) ListStatus(ctx context.Context, status string) ([]AgentApprovalRow, error) {
	rows, err := s.c.query(ctx, agentApprovalSelect+` WHERE "status" = ? ORDER BY "created_at"`, status)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]AgentApprovalRow, 0)
	for rows.Next() {
		row, err := scanAgentApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *row)
	}
	return result, rows.Err()
}

func (s AgentApprovalsStore) Transition(
	ctx context.Context,
	id, from, to, decidedAt string,
) (*AgentApprovalRow, error) {
	var result *AgentApprovalRow
	err := s.c.withImmediate(ctx, func(tx queryExec) error {
		row, err := scanAgentApproval(tx.QueryRowContext(ctx, agentApprovalSelect+` WHERE "id" = ?`, id))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("agent approval not found")
			}
			return err
		}
		if row.Status != from {
			return fmt.Errorf("agent approval is %s, expected %s", row.Status, from)
		}
		updated, err := tx.ExecContext(
			ctx,
			`UPDATE "agent_approval" SET "status" = ?, "decided_at" = ? WHERE "id" = ? AND "status" = ?`,
			to,
			decidedAt,
			id,
			from,
		)
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("agent approval changed concurrently")
		}
		row.Status = to
		row.DecidedAt = &decidedAt
		result = row
		return nil
	})
	return result, err
}

func (s AgentApprovalsStore) Complete(
	ctx context.Context,
	id, from, status, result, message, completedAt string,
) error {
	updated, err := s.c.db.ExecContext(
		ctx,
		`UPDATE "agent_approval" SET "status" = ?, "result" = ?, "error" = ?, "completed_at" = ?
WHERE "id" = ? AND "status" = ?`,
		status,
		result,
		message,
		completedAt,
		id,
		from,
	)
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("agent approval changed concurrently")
	}
	return nil
}

func (s AgentApprovalsStore) CompleteWithEvents(
	ctx context.Context,
	id, from, status, result, message, completedAt string,
	events []AgentEventRow,
) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		updated, err := tx.ExecContext(
			ctx,
			`UPDATE "agent_approval" SET "status" = ?, "result" = ?, "error" = ?, "completed_at" = ?
WHERE "id" = ? AND "status" = ?`,
			status,
			result,
			message,
			completedAt,
			id,
			from,
		)
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("agent approval changed concurrently")
		}
		for _, event := range events {
			var sequence int64
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX("sequence"), 0) + 1 FROM "agent_event" WHERE "session_id" = ?`,
				event.SessionID,
			).Scan(&sequence); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO "agent_event"
("session_id", "sequence", "turn_id", "event_type", "payload", "created_at") VALUES (?, ?, ?, ?, ?, ?)`,
				event.SessionID,
				sequence,
				event.TurnID,
				event.EventType,
				event.Payload,
				event.CreatedAt,
			); err != nil {
				return err
			}
		}
		if len(events) > 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE "agent_session" SET "updated_at" = ? WHERE "id" = ?`,
				completedAt,
				events[0].SessionID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s AgentApprovalsStore) SealExecuting(ctx context.Context, completedAt string) error {
	_, err := s.c.db.ExecContext(ctx, `UPDATE "agent_approval"
SET "status" = 'failed', "error" = 'application restarted while action was executing', "completed_at" = ?
WHERE "status" = 'executing'`, completedAt)
	return err
}

const agentApprovalSelect = `SELECT "id", "session_id", "turn_id", "tool_call_id", "action_id", "arguments",
"summary", "target", "impact", "status", "result", "error", "created_at", "decided_at", "completed_at"
FROM "agent_approval"`
