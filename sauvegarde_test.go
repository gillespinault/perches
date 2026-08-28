package main

import (
	"database/sql"
	"os"
	"testing"
)

// La copie de sauvegarde est une base SQLite complète et cohérente, écrite à côté
// de la base vivante ; le fichier temporaire du renommage n'y traîne pas.
func TestDecision_SauvegardeCoherente(t *testing.T) {
	app, _ := appTest(t)
	l := listeTest(t, app)

	if err := app.sauvegarder(); err != nil {
		t.Fatal(err)
	}
	copie := cheminSauvegarde(app.cheminDB)
	if _, err := os.Stat(copie + ".tmp"); err == nil {
		t.Fatal("fichier temporaire resté en place")
	}

	db, err := sql.Open("sqlite", copie)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var verdict string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&verdict); err != nil || verdict != "ok" {
		t.Fatalf("integrity_check : %q, %v", verdict, err)
	}
	var titre string
	if err := db.QueryRow(`SELECT titre FROM listes WHERE slug = ?`, l.Slug).Scan(&titre); err != nil {
		t.Fatal(err)
	}
	if titre != l.Titre {
		t.Fatalf("titre dans la copie : %q", titre)
	}

	// Une seconde sauvegarde remplace la première sans broncher.
	if err := app.sauvegarder(); err != nil {
		t.Fatal(err)
	}
}
