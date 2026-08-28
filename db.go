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
	// 2026-08-28 (seconde passe) : une perche peut durer plusieurs jours — colonne fin ;
	// la capacité n'existe plus — colonne retirée.
	db.QueryRow(`SELECT count(*) FROM pragma_table_info('intentions') WHERE name = 'fin'`).Scan(&n)
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE intentions ADD COLUMN fin TEXT`); err != nil {
			return err
		}
	}
	db.QueryRow(`SELECT count(*) FROM pragma_table_info('intentions') WHERE name = 'capacite'`).Scan(&n)
	if n > 0 {
		if _, err := db.Exec(`ALTER TABLE intentions DROP COLUMN capacite`); err != nil {
			return err
		}
	}
	// 2026-08-28 (lot F) : tout est repéré, la perche est un geste posé dessus — perche_tendue_le,
	// perche_quand, perche_fin remplacent nature (lot E, éphémère). Les perches existantes reçoivent
	// des dates propres égales à celles de leur événement — ce sont en réalité les dates de l'hôte
	// (Arles), et elles ne bougeront pas si l'événement est corrigé ensuite. Les CHECK ne se posent
	// pas par ALTER : le code les tient.
	db.QueryRow(`SELECT count(*) FROM pragma_table_info('intentions') WHERE name = 'perche_tendue_le'`).Scan(&n)
	if n == 0 {
		for _, col := range []string{"perche_tendue_le", "perche_quand", "perche_fin"} {
			if _, err := db.Exec(`ALTER TABLE intentions ADD COLUMN ` + col + ` TEXT`); err != nil {
				return err
			}
		}
		var avecNature int
		db.QueryRow(`SELECT count(*) FROM pragma_table_info('intentions') WHERE name = 'nature'`).Scan(&avecNature)
		if avecNature > 0 {
			if _, err := db.Exec(`UPDATE intentions SET perche_tendue_le = cree_le, perche_quand = quand, perche_fin = fin WHERE nature = 'perche'`); err != nil {
				return err
			}
			if _, err := db.Exec(`ALTER TABLE intentions DROP COLUMN nature`); err != nil {
				return err
			}
		} else {
			// base d'avant le lot E : tout était perche
			if _, err := db.Exec(`UPDATE intentions SET perche_tendue_le = cree_le, perche_quand = quand, perche_fin = fin`); err != nil {
				return err
			}
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
