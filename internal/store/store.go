// Package store persists imported items and their three versions
// (original / lenient / canonical) in SQLite. The original version is
// insert-only: imported bytes are never overwritten.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Version kinds stored per item.
const (
	KindOriginal  = "original"
	KindLenient   = "lenient"
	KindCanonical = "canonical"
)

type Item struct {
	ID        int64
	Name      string
	Source    string
	CreatedAt time.Time
}

type Version struct {
	ItemID int64
	Kind   string
	Data   []byte
	Digest string
}

type Store struct {
	db *sql.DB
}

var migrations = []string{
	`CREATE TABLE items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE versions (
		item_id INTEGER NOT NULL REFERENCES items(id),
		kind TEXT NOT NULL,
		data BLOB NOT NULL,
		digest TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (item_id, kind)
	)`,
}

// Open opens (creating if needed) the SQLite database and applies migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// CreateItem inserts a new item and returns its id.
func (s *Store) CreateItem(name, source string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO items(name, source, created_at) VALUES(?,?,?)`,
		name, source, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// PutVersion stores a version. The original version is insert-only: a
// duplicate original is silently ignored so imported bytes can never be
// overwritten. Other kinds are replaced.
func (s *Store) PutVersion(itemID int64, kind string, data []byte, digest string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if kind == KindOriginal {
		_, err := s.db.Exec(
			`INSERT OR IGNORE INTO versions(item_id, kind, data, digest, created_at) VALUES(?,?,?,?,?)`,
			itemID, kind, data, digest, now)
		return err
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO versions(item_id, kind, data, digest, created_at) VALUES(?,?,?,?,?)`,
		itemID, kind, data, digest, now)
	return err
}

func (s *Store) ListItems() ([]Item, error) {
	rows, err := s.db.Query(`SELECT id, name, source, created_at FROM items ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var ts string
		if err := rows.Scan(&it.ID, &it.Name, &it.Source, &ts); err != nil {
			return nil, err
		}
		it.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, it)
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("item not found")

// GetItem loads one item and all of its versions keyed by kind.
func (s *Store) GetItem(id int64) (Item, map[string]Version, error) {
	var it Item
	var ts string
	err := s.db.QueryRow(`SELECT id, name, source, created_at FROM items WHERE id=?`, id).
		Scan(&it.ID, &it.Name, &it.Source, &ts)
	if err == sql.ErrNoRows {
		return it, nil, ErrNotFound
	}
	if err != nil {
		return it, nil, err
	}
	it.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	rows, err := s.db.Query(`SELECT kind, data, digest FROM versions WHERE item_id=?`, id)
	if err != nil {
		return it, nil, err
	}
	defer rows.Close()
	vers := map[string]Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.Kind, &v.Data, &v.Digest); err != nil {
			return it, nil, err
		}
		v.ItemID = id
		vers[v.Kind] = v
	}
	return it, vers, rows.Err()
}

// CountItems reports how many items exist (used to seed fixtures once).
func (s *Store) CountItems() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n)
	return n, err
}
