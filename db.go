package main

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func ouvrirDB(chemin string) (*sql.DB, error) {
	if dir := filepath.Dir(chemin); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", chemin+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// une seule connexion : SQLite sérialise les écritures, ça suffit ici
	db.SetMaxOpenConns(1)

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='listes'`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := db.Exec(schemaSQL); err != nil {
			return nil, err
		}
	}
	return db, nil
}
