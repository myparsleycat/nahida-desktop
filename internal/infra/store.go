package infra

import (
	"context"

	"nahida.live/desktop/internal/db"
)

// Store is the application-facing handle around the shipped SQLite client.
type Store struct {
	DB *db.Client
}

func NewStore() *Store {
	return &Store{}
}

// OpenStore opens a caller-supplied file, reconciles the shipped schema, and
// returns a handle around that client. It does not invent a userData path.
func OpenStore(ctx context.Context, path string) (*Store, error) {
	client, err := db.New(path)
	if err != nil {
		return nil, err
	}
	if err := client.Reconcile(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &Store{DB: client}, nil
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	err := s.DB.Close()
	s.DB = nil
	return err
}
