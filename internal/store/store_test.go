package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsAndCRUD(t *testing.T) {
	s := openTemp(t)
	id, err := s.CreateItem("evidence-01", "a0")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.PutVersion(id, KindOriginal, []byte{0xa0}, "digest-o"); err != nil {
		t.Fatalf("put original: %v", err)
	}
	if err := s.PutVersion(id, KindLenient, []byte{0xa0}, "digest-l"); err != nil {
		t.Fatalf("put lenient: %v", err)
	}
	if err := s.PutVersion(id, KindCanonical, []byte{0xa0}, "digest-c"); err != nil {
		t.Fatalf("put canonical: %v", err)
	}
	it, vers, err := s.GetItem(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if it.Name != "evidence-01" || len(vers) != 3 {
		t.Fatalf("item %+v vers %d", it, len(vers))
	}
	items, err := s.ListItems()
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v %d", err, len(items))
	}
}

// The original version is insert-only: imported bytes are never overwritten.
func TestOriginalNeverOverwritten(t *testing.T) {
	s := openTemp(t)
	id, _ := s.CreateItem("x", "01")
	_ = s.PutVersion(id, KindOriginal, []byte{0x01}, "d1")
	_ = s.PutVersion(id, KindOriginal, []byte{0x02}, "d2")
	_, vers, err := s.GetItem(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	v := vers[KindOriginal]
	if len(v.Data) != 1 || v.Data[0] != 0x01 || v.Digest != "d1" {
		t.Fatalf("original overwritten: %+v", v)
	}
	// non-original versions may be replaced
	_ = s.PutVersion(id, KindCanonical, []byte{0x01}, "c1")
	_ = s.PutVersion(id, KindCanonical, []byte{0x02}, "c2")
	_, vers, _ = s.GetItem(id)
	if vers[KindCanonical].Data[0] != 0x02 {
		t.Fatalf("canonical not replaced: %+v", vers[KindCanonical])
	}
}

// Reopening the same file keeps data and does not re-run migrations.
func TestReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id, _ := s.CreateItem("persist", "f6")
	_ = s.PutVersion(id, KindOriginal, []byte{0xf6}, "d")
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	n, err := s2.CountItems()
	if err != nil || n != 1 {
		t.Fatalf("count after reopen: %d %v", n, err)
	}
}
