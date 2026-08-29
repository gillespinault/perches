package main

import (
	"bytes"
	"image"
	"regexp"
	"strings"
	"testing"
)

// La carte d'une liste montre les trois prochains événements, dans l'ordre, avec le bâton
// sur ceux où l'hôte va ; l'adresse de la carte change quand la chronologie change.
func TestDecision_CarteDeListeAvecProchainsEvenements(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	intentionTest(t, app, dans(10), "Les Rencontres d'Arles", "page")
	app.db.Exec(`INSERT INTO intentions (liste_id, jeton, titre, quand, visibilite) VALUES (1, 'repere1', 'KIKK festival', ?, 'page')`, dans(3))
	intentionTest(t, app, dans(20), "Concert", "page")
	intentionTest(t, app, dans(30), "Quatrième", "page")

	intentions, _ := app.intentionsPubliques(1)
	lignes := lignesDeCarte(intentions, 3)
	if len(lignes) != 3 || lignes[0].Titre != "KIKK festival" || lignes[0].Perche || !lignes[1].Perche || lignes[1].Titre != "Les Rencontres d'Arles" {
		t.Fatalf("trois lignes, dans l'ordre, le bâton sur les perches : %+v", lignes)
	}
	if !regexp.MustCompile(`^\d{1,2} [a-zéû]+\.?( \d{4})?$`).MatchString(lignes[0].Date) {
		t.Fatalf("date courte : %q", lignes[0].Date)
	}
	rec := GET(app, "/l/test.jpg")
	img, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	if rec.Code != 200 || err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Fatalf("carte de liste : %d %v", rec.Code, err)
	}

	og := regexp.MustCompile(`og:image" content="([^"]+)"`)
	avant := og.FindStringSubmatch(GET(app, "/l/test").Body.String())[1]
	intentionTest(t, app, dans(1), "Demain", "page")
	apres := og.FindStringSubmatch(GET(app, "/l/test").Body.String())[1]
	if avant == apres {
		t.Fatal("un nouvel événement en tête change l'adresse de la carte")
	}
}

// Les textes décrivent, ils ne revendiquent pas (docs/ecriture.md) : plus de slogan sur les pages.
func TestDecision_PasDeSlogan(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	for _, chemin := range []string{"/", "/l/test", "/a-propos"} {
		if page := GET(app, chemin).Body.String(); strings.Contains(page, "sans obligation") {
			t.Fatalf("%s : slogan « sans obligation » encore présent", chemin)
		}
	}
	if d := descriptionOG("Bonjour,", 2); d != "2 perches." {
		t.Fatalf("description de repli : %q", d)
	}
}
