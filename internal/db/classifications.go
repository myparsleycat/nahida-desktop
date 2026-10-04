package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/samber/lo"
)

type ModClassificationsStore struct{ c *Client }

func (s ModClassificationsStore) ListByGame(ctx context.Context, game string) ([]ModClassificationWithGroups, error) {
	return s.list(ctx, `WHERE c."game" = ?`, game)
}

func (s ModClassificationsStore) FindByID(ctx context.Context, id string) (*ModClassificationWithGroups, error) {
	found, err := s.list(ctx, `WHERE c."id" = ?`, id)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return &found[0], nil
}

func (s ModClassificationsStore) list(
	ctx context.Context,
	where string,
	args ...any,
) ([]ModClassificationWithGroups, error) {
	// One statement keeps classifications and their groups in the same snapshot during concurrent saves.
	rows, err := s.c.query(ctx, `
SELECT c."id", c."game", c."name", c."item_order", c."is_active",
       g."id", g."name", g."item_order"
FROM "mod_classifications" c
LEFT JOIN "mod_classification_groups" g ON g."classification_id" = c."id" `+where+`
ORDER BY c."item_order", c."name", g."item_order", g."name"`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ModClassificationWithGroups{}
	for rows.Next() {
		var row ModClassificationRow
		var active any
		var groupID, groupName sql.NullString
		var groupOrder sql.NullInt64
		if err := rows.Scan(
			&row.ID, &row.Game, &row.Name, &row.ItemOrder, &active, &groupID, &groupName, &groupOrder,
		); err != nil {
			return nil, err
		}
		row.IsActive = toBool(active)
		if len(out) == 0 || out[len(out)-1].ID != row.ID {
			out = append(out, ModClassificationWithGroups{
				ModClassificationRow: row, Groups: []ModClassificationGroupRow{},
			})
		}
		if groupID.Valid {
			index := len(out) - 1
			out[index].Groups = append(out[index].Groups, ModClassificationGroupRow{
				ID: groupID.String, ClassificationID: row.ID, Name: groupName.String, ItemOrder: groupOrder.Int64,
			})
		}
	}
	return out, rows.Err()
}

// Save upserts a classification and replaces its group set in one immediate transaction.
// An existing classification keeps its order and active state; groups missing from the input are deleted.
func (s ModClassificationsStore) Save(
	ctx context.Context,
	row ModClassificationRow,
	groups []ModClassificationGroupRow,
) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO "mod_classifications" ("id", "game", "name", "item_order", "is_active")
VALUES (?, ?, ?, ?, ?)
ON CONFLICT("id") DO UPDATE SET "name" = excluded."name"`,
			row.ID, row.Game, row.Name, row.ItemOrder, lo.Ternary(row.IsActive, 1, 0)); err != nil {
			return err
		}

		kept := make([]any, 0, len(groups)+1)
		kept = append(kept, row.ID)
		for _, group := range groups {
			kept = append(kept, group.ID)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(groups)), ", ")
		if _, err := tx.ExecContext(ctx, `
DELETE FROM "mod_classification_groups"
WHERE "classification_id" = ? AND "id" NOT IN (`+placeholders+`)`, kept...); err != nil {
			return err
		}
		// Park the surviving names so swapped names cannot collide with the unique index mid-update.
		if _, err := tx.ExecContext(ctx, `
UPDATE "mod_classification_groups" SET "name" = char(0) || "id" WHERE "classification_id" = ?`,
			row.ID); err != nil {
			return err
		}

		for _, group := range groups {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO "mod_classification_groups" ("id", "classification_id", "name", "item_order")
VALUES (?, ?, ?, ?)
ON CONFLICT("id") DO UPDATE SET "name" = excluded."name", "item_order" = excluded."item_order"
WHERE "classification_id" = excluded."classification_id"`,
				group.ID, row.ID, group.Name, group.ItemOrder); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s ModClassificationsStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "mod_classifications" WHERE "id" = ?`, id)
}

// SetActive marks one classification of the game as active and clears the rest; a nil id clears all.
func (s ModClassificationsStore) SetActive(ctx context.Context, game string, id *string) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(
			ctx, `UPDATE "mod_classifications" SET "is_active" = 0 WHERE "game" = ?`, game,
		); err != nil {
			return err
		}
		if id == nil {
			return nil
		}
		_, err := tx.ExecContext(
			ctx, `UPDATE "mod_classifications" SET "is_active" = 1 WHERE "game" = ? AND "id" = ?`, game, *id,
		)
		return err
	})
}
