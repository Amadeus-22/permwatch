package filestore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Amadeus-22/permwatch/internal/domain"
)

const addr domain.Address = "klv1uah7ye2sq6vdnlksf6v3q5mtp2yf87352ghy8hve2khxjjcu3vqqaycqgs"

func TestSaveThenLoad(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, found, err := store.Load(ctx, addr); err != nil || found {
		t.Fatalf("empty store: found=%v err=%v", found, err)
	}

	in := domain.Account{Address: addr, Permissions: []domain.Permission{{
		ID: 0, Type: domain.User, Name: "ops", Threshold: 1, Operations: domain.Operations{0x01, 0x03},
		Signers: []domain.Signer{{Address: "klv1x4lhywvzqt4dhz92tps2a2jfrsn2qljz7c89njver2e6nmamkjhq62e787", Weight: 1}},
	}}}
	if err := store.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	out, found, err := store.Load(ctx, addr)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("snapshot changed:\n in  %+v\n out %+v", in, out)
	}

	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("state dir has %d entries, want only the snapshot", len(entries))
	}
}

func TestLoadReportsCorruptSnapshot(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path(addr), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(context.Background(), addr); err == nil {
		t.Fatal("expected an error for a corrupt snapshot")
	}
}
