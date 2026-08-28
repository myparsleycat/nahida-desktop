package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Client is the Go port of the Electron DatabaseClient.
type Client struct {
	db *sql.DB

	Settings                SettingsStore
	AppState                AppStateStore
	GamePaths               GamePathsStore
	ModPresets              ModPresetsStore
	ModPresetItems          ModPresetItemsStore
	ImageCache              ImageCacheStore
	TouchProfileVisionCache TouchProfileVisionCacheStore
	ModScanCache            ModScanCacheStore
	Scripts                 ScriptsStore
	ScriptPresets           ScriptPresetsStore
	ScriptPresetItems       ScriptPresetItemsStore
	ToggleViewerArtifacts   ToggleViewerArtifactsStore
	SchemaState             SchemaStateStore
}

// New opens a file-backed database with the shipped opener.
func New(path string) (*Client, error) {
	sqlDB, err := Open(path)
	if err != nil {
		return nil, err
	}
	return newClient(sqlDB), nil
}

func newClient(sqlDB *sql.DB) *Client {
	c := &Client{db: sqlDB}
	c.Settings = SettingsStore{c: c}
	c.AppState = AppStateStore{c: c}
	c.GamePaths = GamePathsStore{c: c}
	c.ModPresets = ModPresetsStore{c: c}
	c.ModPresetItems = ModPresetItemsStore{c: c}
	c.ImageCache = ImageCacheStore{c: c}
	c.TouchProfileVisionCache = TouchProfileVisionCacheStore{c: c}
	c.ModScanCache = ModScanCacheStore{c: c}
	c.Scripts = ScriptsStore{c: c}
	c.ScriptPresets = ScriptPresetsStore{c: c}
	c.ScriptPresetItems = ScriptPresetItemsStore{c: c}
	c.ToggleViewerArtifacts = ToggleViewerArtifactsStore{c: c}
	c.SchemaState = SchemaStateStore{c: c}
	return c
}

func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *Client) SQL() *sql.DB {
	return c.db
}

func (c *Client) exec(ctx context.Context, query string, args ...any) error {
	_, err := c.db.ExecContext(ctx, query, args...)
	return err
}

func (c *Client) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, query, args...)
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

type queryExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (c *Client) withImmediate(ctx context.Context, fn func(queryExec) error) error {
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin immediate: %w", err)
	}
	if err := fn(conn); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
