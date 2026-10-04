// Package filestore keeps one JSON snapshot per account in a directory.
package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

// Store writes snapshots under Dir, one file per address.
type Store struct{ Dir string }

// New creates the directory if needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) path(addr domain.Address) string {
	return filepath.Join(s.Dir, string(addr)+".json")
}

// Load reads the snapshot of addr; found is false when none was saved yet.
func (s *Store) Load(_ context.Context, addr domain.Address) (domain.Account, bool, error) {
	raw, err := os.ReadFile(s.path(addr))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Account{}, false, nil
	}
	if err != nil {
		return domain.Account{}, false, fmt.Errorf("read snapshot: %w", err)
	}
	var account domain.Account
	if err := json.Unmarshal(raw, &account); err != nil {
		return domain.Account{}, false, fmt.Errorf("decode snapshot %s: %w", s.path(addr), err)
	}
	return account, true, nil
}

// Save replaces the snapshot atomically: a crash leaves the old file or the new
// one, never a partial write.
func (s *Store) Save(_ context.Context, account domain.Account) error {
	raw, err := json.MarshalIndent(account.Normalized(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".snapshot-*")
	if err != nil {
		return fmt.Errorf("create temp snapshot: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path(account.Address)); err != nil {
		return fmt.Errorf("replace snapshot: %w", err)
	}
	return nil
}
