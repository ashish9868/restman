package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"restman/internal/id"
	"restman/internal/model"
)

type Store struct {
	db *sql.DB
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "restman.db"))
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS history (
			id TEXT PRIMARY KEY,
			sent_at TEXT NOT NULL,
			method TEXT,
			url TEXT,
			status INTEGER,
			duration_ms INTEGER,
			name TEXT,
			request_id TEXT,
			request_json TEXT
		);
		CREATE TABLE IF NOT EXISTS cookies (
			domain TEXT,
			path TEXT,
			name TEXT,
			value TEXT,
			expires TEXT,
			secure INTEGER,
			http_only INTEGER,
			PRIMARY KEY(domain, path, name)
		);
		CREATE TABLE IF NOT EXISTS ui_state (
			key TEXT PRIMARY KEY,
			value TEXT
		);
	`)
	return err
}

func (s *Store) AddHistory(entry model.HistoryEntry, request model.Request) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = id.New()
	}
	if entry.SentAt == "" {
		entry.SentAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err = s.db.Exec(`INSERT INTO history (id, sent_at, method, url, status, duration_ms, name, request_id, request_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.SentAt, entry.Method, entry.URL, entry.Status, entry.DurationMS, entry.Name, entry.RequestID, string(data))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM history WHERE id NOT IN (SELECT id FROM history ORDER BY sent_at DESC LIMIT 200)`)
	return err
}

func (s *Store) History(limit int) ([]model.HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id, sent_at, method, url, status, duration_ms, name, request_id FROM history ORDER BY sent_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.HistoryEntry
	for rows.Next() {
		var entry model.HistoryEntry
		if err := rows.Scan(&entry.ID, &entry.SentAt, &entry.Method, &entry.URL, &entry.Status, &entry.DurationMS, &entry.Name, &entry.RequestID); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	if out == nil {
		out = []model.HistoryEntry{}
	}
	return out, rows.Err()
}

func (s *Store) HistoryRequest(historyID string) (model.Request, error) {
	var raw string
	err := s.db.QueryRow(`SELECT request_json FROM history WHERE id = ?`, historyID).Scan(&raw)
	if err != nil {
		return model.Request{}, err
	}
	var req model.Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return model.Request{}, err
	}
	req.Normalize()
	return req, nil
}

func (s *Store) ClearHistory() error {
	_, err := s.db.Exec(`DELETE FROM history`)
	return err
}

func (s *Store) DeleteHistory(id string) error {
	_, err := s.db.Exec(`DELETE FROM history WHERE id = ?`, id)
	return err
}

func (s *Store) SaveCookies(cookies []model.Cookie) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, cookie := range cookies {
		if cookie.Path == "" {
			cookie.Path = "/"
		}
		_, err := tx.Exec(`INSERT INTO cookies (domain, path, name, value, expires, secure, http_only)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(domain, path, name) DO UPDATE SET value = excluded.value, expires = excluded.expires, secure = excluded.secure, http_only = excluded.http_only`,
			cookie.Domain, cookie.Path, cookie.Name, cookie.Value, cookie.Expires, boolInt(cookie.Secure), boolInt(cookie.HTTPOnly))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Cookies() ([]model.Cookie, error) {
	rows, err := s.db.Query(`SELECT domain, path, name, value, expires, secure, http_only FROM cookies ORDER BY domain, path, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Cookie
	for rows.Next() {
		var cookie model.Cookie
		var secure, httpOnly int
		if err := rows.Scan(&cookie.Domain, &cookie.Path, &cookie.Name, &cookie.Value, &cookie.Expires, &secure, &httpOnly); err != nil {
			return nil, err
		}
		cookie.Secure = secure == 1
		cookie.HTTPOnly = httpOnly == 1
		out = append(out, cookie)
	}
	if out == nil {
		out = []model.Cookie{}
	}
	return out, rows.Err()
}

func (s *Store) DeleteCookie(domain, path, name string) error {
	_, err := s.db.Exec(`DELETE FROM cookies WHERE domain = ? AND path = ? AND name = ?`, domain, path, name)
	return err
}

func (s *Store) GetState(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM ui_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetState(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO ui_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "restman"), nil
}

func DefaultWorkspace() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return filepath.Join(home, "Restman"), nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
