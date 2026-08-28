package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

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
	if err := migrer(db); err != nil {
		return nil, err
	}
	return db, nil
}

// migrer applique les évolutions de schéma que SQLite ne sait pas faire par ALTER.
// 2026-08-28 : troisième statut « jaurais_aime » — la contrainte CHECK de `reponses`
// est réécrite en reconstruisant la table (procédure recommandée par SQLite).
func migrer(db *sql.DB) error {
	var def string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='reponses'`).Scan(&def); err != nil {
		return err
	}
	// 2026-08-28 : « fermer ma liste » — colonne fermee_le.
	var n int
	db.QueryRow(`SELECT count(*) FROM pragma_table_info('listes') WHERE name = 'fermee_le'`).Scan(&n)
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE listes ADD COLUMN fermee_le TEXT`); err != nil {
			return err
		}
	}
	if strings.Contains(def, "jaurais_aime") {
		return nil
	}
	_, err := db.Exec(`
		PRAGMA foreign_keys = OFF;
		BEGIN;
		CREATE TABLE reponses_v2 (
		    id             INTEGER PRIMARY KEY,
		    intention_id   INTEGER NOT NULL REFERENCES intentions(id) ON DELETE CASCADE,
		    prenom         TEXT NOT NULL,
		    statut         TEXT NOT NULL CHECK (statut IN ('jy_serai', 'peut_etre', 'jaurais_aime')),
		    mot            TEXT NOT NULL DEFAULT '',
		    prenom_visible INTEGER NOT NULL DEFAULT 1,
		    email          TEXT,
		    cree_le        TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO reponses_v2 SELECT id, intention_id, prenom, statut, mot, prenom_visible, email, cree_le FROM reponses;
		DROP TABLE reponses;
		ALTER TABLE reponses_v2 RENAME TO reponses;
		CREATE INDEX IF NOT EXISTS reponses_intention ON reponses(intention_id);
		COMMIT;
		PRAGMA foreign_keys = ON;`)
	return err
}
