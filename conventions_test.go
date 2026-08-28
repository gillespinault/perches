package main

// Suite de tests des conventions (docs/tests-conventions.md).
// Cette suite est un outil de gouvernance : une PR qui modifie un de ces tests
// est une PR qui demande à changer le produit.

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ---- outillage ----

type EnvoiTest struct{ Dest, Sujet, Corps string }

type MailerTest struct{ Envois []EnvoiTest }

func (m *MailerTest) Envoyer(dest, sujet, corps string) error {
	m.Envois = append(m.Envois, EnvoiTest{dest, sujet, corps})
	return nil
}

func appTest(t *testing.T) (*App, *MailerTest) {
	t.Helper()
	db, err := ouvrirDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := &MailerTest{}
	return &App{
		db: db, mailer: m, tpl: chargerTemplates(),
		politique: "ouverte", baseURL: "http://perches.test", limiteur: nouveauLimiteur(),
		synchrone: true, csp: cspPour(chargerTemplates()),
	}, m
}

func listeTest(t *testing.T, app *App) *Liste {
	t.Helper()
	_, err := app.db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat)
		VALUES ('test', 'edtest', 'Les perches de Test', 'Bonjour, voici mes envies du moment.', 'je rouvre doucement')`)
	if err != nil {
		t.Fatal(err)
	}
	l, err := app.listeParSlug("test")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

var compteurJeton int

func intentionTest(t *testing.T, app *App, quand, titre, visibilite string) *Intention {
	t.Helper()
	compteurJeton++
	j := fmt.Sprintf("jetontest%d", compteurJeton)
	_, err := app.db.Exec(`INSERT INTO intentions (liste_id, jeton, titre, description, quand, lieu, visibilite,
		perche_tendue_le, perche_quand) VALUES (1, ?, ?, 'On ira voir les machines ensemble.', ?, 'Namur', ?, datetime('now'), ?)`,
		j, titre, quand, visibilite, quand)
	if err != nil {
		t.Fatal(err)
	}
	i, _, err := app.intentionParJeton(j)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func repereTest(t *testing.T, app *App, quand, titre string) *Intention {
	t.Helper()
	compteurJeton++
	j := fmt.Sprintf("jetontest%d", compteurJeton)
	if _, err := app.db.Exec(`INSERT INTO intentions (liste_id, jeton, titre, description, quand, lieu, visibilite)
		VALUES (1, ?, ?, 'Repéré, à voir.', ?, 'Namur', 'page')`, j, titre, quand); err != nil {
		t.Fatal(err)
	}
	i, _, err := app.intentionParJeton(j)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func GET(app *App, chemin string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.handler().ServeHTTP(rec, httptest.NewRequest("GET", chemin, nil))
	return rec
}

func POST(app *App, chemin string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", chemin, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.handler().ServeHTTP(rec, req)
	return rec
}

func dans(jours int) string { return time.Now().AddDate(0, 0, jours).Format("2006-01-02") }
func ilYA(jours int) string { return time.Now().AddDate(0, 0, -jours).Format("2006-01-02") }

func repondreTest(t *testing.T, app *App, jetonI, prenom, statut, mot, email string) {
	t.Helper()
	form := url.Values{"prenom": {prenom}, "statut": {statut}, "mot": {mot},
		"prenom_visible": {"1"}, "email": {email}}
	rec := POST(app, "/i/"+jetonI+"/reponses", form)
	if rec.Code != 303 {
		t.Fatalf("réponse de %s refusée : %d %s", prenom, rec.Code, rec.Body.String())
	}
}

// ---- C1 : le silence vaut « pas cette fois » ----

func TestConv01_LeSystemeIgnoreLesNonRepondants(t *testing.T) {
	app, _ := appTest(t)
	rows, err := app.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	interdits := []string{"relance", "attente", "attendu", "vu_le", "lu_le"}
	for _, table := range tables {
		cols, err := app.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var c string
			cols.Scan(&c)
			for _, mot := range interdits {
				if strings.Contains(c, mot) {
					t.Errorf("colonne %s.%s : le système modélise l'attente, il ne doit pas", table, c)
				}
			}
		}
		cols.Close()
	}
}

func TestConv01_AucuneRelance(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(1), "KIKK", "page")
	// une réponse sans e-mail : rien ne doit partir, jamais
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "")
	app.envoyerRappels()
	if len(m.Envois) != 0 {
		t.Fatalf("un envoi est parti sans e-mail donné : %+v", m.Envois)
	}
	// avec e-mail : le rappel de la veille part, une seule fois (idempotence)
	repondreTest(t, app, i.Jeton, "Marc", "peut_etre", "", "marc@exemple.be")
	app.envoyerRappels()
	app.envoyerRappels()
	if len(m.Envois) != 1 {
		t.Fatalf("attendu exactement 1 rappel, reçu %d", len(m.Envois))
	}
}

// ---- C2 : aucun signal négatif ----

func TestConv02_AucunBoutonNon(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	body := GET(app, "/i/"+i.Jeton).Body.String()
	bas := strings.ToLower(body)
	if strings.Contains(bas, `value="non"`) || strings.Contains(bas, ">non<") {
		t.Fatal("un contrôle de refus existe sur la page")
	}
	if n := strings.Count(body, `name="statut"`); n != 3 {
		t.Fatalf("attendu exactement 3 choix (j'y serai, peut-être, j'aurais bien aimé), trouvé %d", n)
	}
	for _, v := range []string{"jy_serai", "peut_etre", "jaurais_aime"} {
		if !strings.Contains(body, `value="`+v+`"`) {
			t.Fatalf("le choix %s manque", v)
		}
	}
}

func TestConv02_PostNonRejete(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Marc"}, "statut": {"non"}})
	if rec.Code != 400 {
		t.Fatalf("statut=non doit être rejeté (400), reçu %d", rec.Code)
	}
}

func TestConv02_SchemaRefuseLeNon(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	_, err := app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut) VALUES (?, 'Marc', 'non')`, i.ID)
	if err == nil {
		t.Fatal("le schéma a accepté un statut « non »")
	}
}

func TestConv02_RienNeSeRemplit(t *testing.T) {
	// Seconde passe (2026-08-28) : la capacité n'existe plus. Quel que soit le nombre de réponses,
	// le formulaire reste, et le mot « complet » n'apparaît jamais : ce serait un signal négatif.
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "Table au restaurant", "page")
	for _, prenom := range []string{"Anna", "Marc", "Léa"} {
		repondreTest(t, app, i.Jeton, prenom, "jy_serai", "", "")
	}
	body := GET(app, "/i/"+i.Jeton).Body.String()
	if !strings.Contains(body, `name="statut"`) {
		t.Fatal("le formulaire a disparu")
	}
	if regexp.MustCompile(`(?i)\bcomplet`).MatchString(body) { // le mot, pas l'attribut autocomplete
		t.Fatal("la page affiche « complet » — c'est un signal négatif")
	}
	repondreTest(t, app, i.Jeton, "Jo", "peut_etre", "", "")
}

// ---- C3 : pas de fil de discussion ----

func TestConv03_MotUneLigneMax(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	rec := POST(app, "/i/"+i.Jeton+"/reponses",
		url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}, "mot": {"ligne un\nligne deux"}})
	if rec.Code != 400 {
		t.Fatalf("un mot multi-lignes doit être refusé, reçu %d", rec.Code)
	}
	rec = POST(app, "/i/"+i.Jeton+"/reponses",
		url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}, "mot": {strings.Repeat("a", 201)}})
	if rec.Code != 400 {
		t.Fatalf("un mot de 201 caractères doit être refusé, reçu %d", rec.Code)
	}
}

func TestConv03_MotsInvisiblesDesAutresInvites(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "merci pour la perche", "")
	if strings.Contains(GET(app, "/i/"+i.Jeton).Body.String(), "merci pour la perche") {
		t.Fatal("le mot est visible sur la page publique")
	}
	if !strings.Contains(GET(app, "/e/edtest").Body.String(), "merci pour la perche") {
		t.Fatal("l'hôte ne voit pas le mot")
	}
}

func TestConv03_AucunBoutonRepondre(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "quel plaisir", "")
	if strings.Contains(strings.ToLower(GET(app, "/e/edtest").Body.String()), "répondre") {
		t.Fatal("la vue hôte suggère de répondre au mot — le mot se lit, sans suite")
	}
}

// ---- C4 : capacité indicative ----

func TestConv04_PasDeCapacite(t *testing.T) {
	// Convention 4 retirée le 2026-08-28 : une perche ne compte pas les places. Ni colonne,
	// ni champ, ni mention — un nombre de places serait déjà une manière de dire non.
	app, _ := appTest(t)
	listeTest(t, app)
	var n int
	app.db.QueryRow(`SELECT count(*) FROM pragma_table_info('intentions') WHERE name = 'capacite'`).Scan(&n)
	if n != 0 {
		t.Fatal("la colonne capacite existe encore")
	}
	if page := GET(app, "/e/edtest").Body.String(); strings.Contains(page, `name="capacite"`) || strings.Contains(strings.ToLower(page), "places") {
		t.Fatal("l'édition parle encore de places")
	}
}

// ---- C5 : « j'y vais de toute façon » par défaut ----

func TestConv05_DefautSchemaEtAffichage(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	rec := POST(app, "/e/edtest/intentions", url.Values{"tendre": {"1"}, "titre": {"Expo"}, "date": {dans(3)}})
	if rec.Code != 303 {
		t.Fatalf("création refusée : %d %s", rec.Code, rec.Body.String())
	}
	var jyVais bool
	var j string
	if err := app.db.QueryRow(`SELECT jy_vais_de_toute_facon, jeton FROM intentions WHERE titre='Expo'`).
		Scan(&jyVais, &j); err != nil {
		t.Fatal(err)
	}
	if !jyVais {
		t.Fatal("sans rien préciser, « j'y vais de toute façon » doit valoir vrai")
	}
	if !strings.Contains(GET(app, "/i/"+j).Body.String(), "de toute façon") {
		t.Fatal("la page n'affiche pas « j'y vais de toute façon »")
	}
}

// ---- C6 : l'état de l'hôte est visible ----

func TestConv06_LaVoixDeLHoteSurChaquePage(t *testing.T) {
	// Décision 2026-08-28 : plus de champ « état » — l'hôte dit où il en est dans son introduction,
	// et cette introduction accompagne chaque perche.
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	if !strings.Contains(GET(app, "/l/test").Body.String(), "voici mes envies du moment") {
		t.Fatal("l'introduction manque sur la page de la liste")
	}
	if !strings.Contains(GET(app, "/i/"+i.Jeton).Body.String(), "voici mes envies du moment") {
		t.Fatal("l'introduction manque sur la page de la perche")
	}
	if strings.Contains(GET(app, "/l/test").Body.String(), "je rouvre doucement") {
		t.Fatal("l'état n'est plus affiché")
	}
}

// ---- C7 : aucune notification poussée vers l'hôte ----

func TestConv07_ReponseSansEnvoi(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "on se réjouit", "anna@exemple.be")
	if len(m.Envois) != 0 {
		t.Fatalf("une réponse a déclenché %d envoi(s) — l'hôte va chercher l'information quand il veut", len(m.Envois))
	}
}

// ---- C8 : l'invité n'a jamais de compte ----

func TestConv08_ReponseSansCookieNiSession(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}})
	if rec.Code != 303 {
		t.Fatalf("répondre sans cookie ni session doit suffire, reçu %d", rec.Code)
	}
	if rec.Header().Get("Set-Cookie") != "" || GET(app, "/i/"+i.Jeton).Header().Get("Set-Cookie") != "" {
		t.Fatal("le serveur pose un cookie — l'invité n'a pas d'identité à porter")
	}
}

// ---- C9 : ce qui est passé disparaît de la page ; l'hôte garde ses lettres ----

func TestConv09_IntentionPasseeAbsenteDuPublic(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	intentionTest(t, app, ilYA(2), "Vernissage", "page")
	if strings.Contains(GET(app, "/l/test").Body.String(), "Vernissage") {
		t.Fatal("une intention passée reste sur la page publique")
	}
	if !strings.Contains(GET(app, "/e/edtest").Body.String(), "Vernissage") {
		t.Fatal("l'archive de l'hôte a perdu l'intention passée")
	}
}

func TestConv09_ArchiveViaJetonEdition(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, ilYA(2), "Vernissage", "page")
	app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut, mot) VALUES (?, 'Anna', 'jy_serai', 'belle soirée')`, i.ID)
	body := GET(app, "/e/edtest").Body.String()
	if !strings.Contains(body, "Anna") || !strings.Contains(body, "belle soirée") {
		t.Fatal("l'hôte doit garder ses lettres : prénoms et mots dans l'archive")
	}
}

func TestConv09_ReponsesPubliquesEffaceesApresDelai(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, ilYA(40), "Vernissage", "page")
	app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut, prenom_visible) VALUES (?, 'Anna', 'jy_serai', 1)`, i.ID)
	rec := GET(app, "/i/"+i.Jeton)
	if rec.Code != 200 {
		t.Fatalf("la page d'une intention passée reste accessible par lien, reçu %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Anna") {
		t.Fatal("après le délai, aucun prénom ne doit rester sur une page publique")
	}
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE intention_id = ?`, i.ID).Scan(&n)
	if n != 1 {
		t.Fatal("l'effacement public ne doit pas détruire l'archive de l'hôte")
	}
}

// ---- C10 : la page est une lettre avant d'être un formulaire ----

func TestConv10_LettreAvantMecanique(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	body := GET(app, "/l/test").Body.String()
	lettre, mecanique := strings.Index(body, "voici mes envies"), strings.Index(body, "KIKK")
	if lettre < 0 || mecanique < 0 || lettre > mecanique {
		t.Fatal("sur la liste, la lettre doit précéder les intentions")
	}
	body = GET(app, "/i/"+i.Jeton).Body.String()
	desc, form := strings.Index(body, "machines ensemble"), strings.Index(body, "<form")
	if desc < 0 || form < 0 || desc > form {
		t.Fatal("sur l'intention, le texte doit précéder le formulaire")
	}
}

// ---- C11 : logistique oui, social jamais ----

func TestConv11_AnnulationNotifiee(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "anna@exemple.be")
	repondreTest(t, app, i.Jeton, "Marc", "peut_etre", "", "")
	rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/annuler", i.ID), nil)
	if rec.Code != 303 {
		t.Fatalf("annulation refusée : %d", rec.Code)
	}
	if len(m.Envois) != 1 || m.Envois[0].Dest != "anna@exemple.be" {
		t.Fatalf("attendu exactement 1 avis logistique vers anna@exemple.be, reçu %+v", m.Envois)
	}
	// idempotence : annuler deux fois n'écrit pas deux fois
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/annuler", i.ID), nil)
	if len(m.Envois) != 1 {
		t.Fatalf("une seconde annulation a renvoyé un courriel")
	}
	var n int
	app.db.QueryRow(`SELECT count(*) FROM envois WHERE type='logistique'`).Scan(&n)
	if n != 1 {
		t.Fatalf("journal envois : attendu 1 ligne logistique, trouvé %d", n)
	}
}

func TestConv11_RienDeSocialNEstEnvoye(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "un mot gentil", "anna@exemple.be")
	repondreTest(t, app, i.Jeton, "Marc", "peut_etre", "", "marc@exemple.be")
	GET(app, "/e/edtest")
	if len(m.Envois) != 0 {
		t.Fatalf("du social est parti : %+v", m.Envois)
	}
}

func TestConv11_TypesDEnvoiBornes(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	if _, err := app.db.Exec(`INSERT INTO envois (liste_id, type) VALUES (1, 'nouveau_participant')`); err == nil {
		t.Fatal("le schéma a accepté un type d'envoi social")
	}
}

// ---- décisions annexes ----

func TestDecision_CarteOGToujoursComplete(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "Expo discrète", "lien")
	body := GET(app, "/i/"+i.Jeton).Body.String()
	if !strings.Contains(body, `property="og:title"`) || !strings.Contains(body, "Expo discrète") {
		t.Fatal("og:title incomplet pour une intention « lien seulement »")
	}
	if !strings.Contains(body, `property="og:description"`) || !strings.Contains(body, "Namur") {
		t.Fatal("og:description doit porter date et lieu, même en « lien seulement »")
	}
}

func TestDecision_LienSeulementAbsentDeLaListe(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "Expo discrète", "lien")
	if strings.Contains(GET(app, "/l/test").Body.String(), "Expo discrète") {
		t.Fatal("une intention « lien seulement » apparaît sur la page publique")
	}
	if rec := GET(app, "/i/"+i.Jeton); rec.Code != 200 {
		t.Fatalf("la perche directe doit rester accessible, reçu %d", rec.Code)
	}
}

func TestDecision_HoteEffaceUneReponse(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "")
	var idReponse int64
	app.db.QueryRow(`SELECT id FROM reponses WHERE intention_id = ?`, i.ID).Scan(&idReponse)
	if rec := POST(app, fmt.Sprintf("/e/mauvaisjeton/reponses/%d/effacer", idReponse), nil); rec.Code != 404 {
		t.Fatalf("un jeton inconnu doit donner 404, reçu %d", rec.Code)
	}
	if rec := POST(app, fmt.Sprintf("/e/edtest/reponses/%d/effacer", idReponse), nil); rec.Code != 303 {
		t.Fatalf("l'hôte doit pouvoir effacer, reçu %d", rec.Code)
	}
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE id = ?`, idReponse).Scan(&n)
	if n != 0 {
		t.Fatal("la réponse n'a pas été effacée")
	}
}

func TestDecision_SansEmailLaPageLeDit(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}})
	rec = GET(app, rec.Header().Get("Location"))
	if !strings.Contains(strings.ToLower(rec.Body.String()), "revérifie la page avant d'y aller") {
		t.Fatal("sans e-mail, la page doit dire de revérifier avant d'y aller")
	}
}

func TestDecision_ExportsToujoursDisponibles(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	intentionTest(t, app, dans(3), "KIKK", "page")
	rec := GET(app, "/l/test.ics")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "BEGIN:VCALENDAR") {
		t.Fatalf("export ICS indisponible : %d", rec.Code)
	}
	rec = GET(app, "/l/test.json")
	if rec.Code != 200 || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("export JSON indisponible ou invalide : %d", rec.Code)
	}
}

func TestDecision_IntentionOuverteExigeEcheance(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	if _, err := app.db.Exec(`INSERT INTO intentions (liste_id, jeton, titre) VALUES (1, 'ouverte1', 'Sans borne')`); err == nil {
		t.Fatal("une intention sans date ni échéance doit être impossible")
	}
	if _, err := app.db.Exec(`INSERT INTO intentions (liste_id, jeton, titre, echeance_decision)
		VALUES (1, 'ouverte2', 'À fixer', ?)`, dans(10)); err != nil {
		t.Fatalf("une intention ouverte avec échéance doit passer : %v", err)
	}
}

func TestDecision_CodeInvitationRequis(t *testing.T) {
	app, _ := appTest(t)
	app.politique = "invitation"
	form := url.Values{"titre": {"Les perches d'Anna"}, "email": {"anna@exemple.be"}}
	if rec := POST(app, "/listes", form); rec.Code != 403 {
		t.Fatalf("sans code, la création doit être refusée, reçu %d", rec.Code)
	}
	app.db.Exec(`INSERT INTO codes_invitation (code) VALUES ('sesame')`)
	form.Set("code", "sesame")
	if rec := POST(app, "/listes", form); rec.Code != 303 {
		t.Fatalf("avec un code valide, la création doit passer, reçu %d", rec.Code)
	}
	form2 := url.Values{"titre": {"Encore"}, "code": {"sesame"}, "email": {"anna@exemple.be"}}
	if rec := POST(app, "/listes", form2); rec.Code != 403 {
		t.Fatalf("un code déjà utilisé doit être refusé, reçu %d", rec.Code)
	}
}

func TestDecision_AccueilSelonDOuOnArrive(t *testing.T) {
	app, _ := appTest(t)
	app.politique = "invitation"
	accueil := GET(app, "/").Body.String()
	if strings.Contains(accueil, `name="titre"`) {
		t.Fatal("sans code, pas de formulaire d'ouverture")
	}
	if !strings.Contains(accueil, `action="/recuperation"`) || !strings.Contains(accueil, "sur invitation") {
		t.Fatal("sans code : retrouver son atelier par e-mail, et savoir que l'ouverture est sur invitation")
	}
	app.db.Exec(`INSERT INTO codes_invitation (code) VALUES ('sesame')`)
	avecCode := GET(app, "/?code=sesame").Body.String()
	if !strings.Contains(avecCode, `name="titre"`) || !strings.Contains(avecCode, `value="sesame"`) {
		t.Fatal("le lien d'invitation ouvre le formulaire, code prérempli")
	}
	form := url.Values{"titre": {"Les perches de Léa"}, "code": {"sesame"}, "email": {"lea@exemple.be"}}
	rec := POST(app, "/listes", form)
	loc := rec.Header().Get("Location")
	if rec.Code != 303 || !strings.HasPrefix(loc, "/e/") {
		t.Fatalf("la création mène directement à l'édition, reçu %d %q", rec.Code, loc)
	}
	page := GET(app, loc).Body.String()
	if !strings.Contains(page, "/l/lea") || !strings.Contains(page, strings.TrimSuffix(loc, "?bienvenue=1")) {
		t.Fatal("l'édition de bienvenue montre les deux liens : public et secret")
	}
}

func TestConv02_AhZutEstUneVraieReponse(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(1), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jaurais_aime", "une prochaine fois", "anna@exemple.be")
	page := GET(app, "/i/"+i.Jeton).Body.String()
	if !strings.Contains(page, "Anna aurait bien aimé.") {
		t.Fatal("« j'aurais bien aimé » s'affiche comme les autres réponses")
	}
	if !strings.Contains(GET(app, "/e/edtest").Body.String(), "une prochaine fois") {
		t.Fatal("l'hôte lit le mot")
	}
	app.envoyerRappels()
	if len(m.Envois) != 0 {
		t.Fatal("pas de rappel de la veille à qui ne vient pas")
	}
}

func TestDecision_PotDeMiel(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Bot"}, "statut": {"jy_serai"}, "verif": {"http://spam"}})
	if rec.Code != 303 {
		t.Fatalf("un robot qui remplit le champ caché est redirigé sans bruit, reçu %d", rec.Code)
	}
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses`).Scan(&n)
	if n != 0 {
		t.Fatal("la réponse du robot ne doit pas être enregistrée")
	}
}

func TestDecision_MigrationTroisiemeStatut(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	ancien := strings.Replace(schemaSQL, "'peut_etre', 'jaurais_aime'", "'peut_etre'", 1)
	if _, err := db.Exec(ancien); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat) VALUES ('t','e','T','','')`)
	db.Exec(`INSERT INTO intentions (liste_id, jeton, titre, quand) VALUES (1,'j','I','2030-01-01T10:00')`)
	db.Exec(`INSERT INTO reponses (intention_id, prenom, statut) VALUES (1,'Anna','jy_serai')`)
	if err := migrer(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO reponses (intention_id, prenom, statut) VALUES (1,'Léa','jaurais_aime')`); err != nil {
		t.Fatalf("après migration le troisième statut passe : %v", err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM reponses`).Scan(&n)
	if n != 2 {
		t.Fatalf("les réponses existantes sont conservées, trouvé %d", n)
	}
}

func TestDecision_LeNavigateurDeLHoteRetientSonAtelier(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	rec := GET(app, "/e/edtest")
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "atelier" || cookies[0].Value != "edtest" || !cookies[0].HttpOnly {
		t.Fatalf("l'atelier pose un cookie HttpOnly « atelier », reçu %v", cookies)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	app.handler().ServeHTTP(w, req)
	if w.Code != 303 || w.Header().Get("Location") != "/e/edtest" {
		t.Fatalf("l'accueil mène droit à l'atelier retenu, reçu %d %s", w.Code, w.Header().Get("Location"))
	}
	if GET(app, "/").Header().Get("Set-Cookie") != "" || GET(app, "/l/test").Header().Get("Set-Cookie") != "" {
		t.Fatal("hors de l'atelier, aucune page ne pose de cookie")
	}
	if rec := POST(app, "/oublier", nil); rec.Code != 303 || !strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("« oublier » efface le cookie")
	}
}

func TestDecision_AdresseDeriveeDuTitre(t *testing.T) {
	cas := map[string]string{
		"Les perches de Léa":     "lea",
		"Les perche de Gilles":   "gilles",
		"La liste d'Anna":        "anna",
		"Les perches d'Émile":    "emile",
		"Perches de Gilles":      "gilles",
		"Ça, c'est l'été !":      "ca-c-est-l-ete",
		"   ":                    "liste",
		"Les perches de ":        "les-perches-de",
		"Œuvres & cætera — 2026": "ouvres-catera-2026",
		strings.Repeat("a", 60):  strings.Repeat("a", 40),
	}
	for titre, attendu := range cas {
		if got := slugDe(titre); got != attendu {
			t.Errorf("slugDe(%q) = %q, attendu %q", titre, got, attendu)
		}
	}
	app, _ := appTest(t)
	app.politique = "ouverte"
	for k, attendu := range []string{"/e/", "/e/", "/e/"} {
		rec := POST(app, "/listes", url.Values{"titre": {"Les perches d'Anna"}, "email": {fmt.Sprintf("anna%d@exemple.be", k)}})
		if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), attendu) {
			t.Fatalf("création : %d %s", rec.Code, rec.Header().Get("Location"))
		}
	}
	var slugs []string
	rows, _ := app.db.Query(`SELECT slug FROM listes ORDER BY id`)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		slugs = append(slugs, s)
	}
	rows.Close()
	if strings.Join(slugs, " ") != "anna anna-2 anna-3" {
		t.Fatalf("dédoublonnage attendu anna anna-2 anna-3, trouvé %v", slugs)
	}
}

func TestDecision_UnHoteOuvreLaPorteAUnAmi(t *testing.T) {
	app, _ := appTest(t)
	app.politique = "invitation"
	listeTest(t, app)
	rec := POST(app, "/e/edtest/invitations", nil)
	loc := rec.Header().Get("Location")
	code := strings.TrimPrefix(loc, "/e/edtest/inviter?invitation=")
	if rec.Code != 303 || len(code) != 16 {
		t.Fatalf("redirection inattendue : %d %q", rec.Code, loc)
	}
	if !strings.Contains(GET(app, loc).Body.String(), "/?code="+code) {
		t.Fatal("la page « inviter » montre le lien à envoyer")
	}
	if !strings.Contains(GET(app, "/?code="+code).Body.String(), `name="titre"`) {
		t.Fatal("le lien d'invitation ouvre le formulaire")
	}
	if rec := POST(app, "/e/inconnu/invitations", nil); rec.Code != 404 {
		t.Fatalf("sans clé d'atelier valide, pas d'invitation, reçu %d", rec.Code)
	}
}

func TestDecision_ReglagesTitreAdresseEmail(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	form := url.Values{"titre": {"Les perches de Test, corrigé"}, "slug": {"test-corrige"}, "email": {"test@exemple.be"}}
	if rec := POST(app, "/e/edtest/reglages", form); rec.Code != 303 {
		t.Fatalf("réglages : %d", rec.Code)
	}
	l, _ := app.listeParJetonEdition("edtest")
	if l.Slug != "test-corrige" || l.Titre != "Les perches de Test, corrigé" || l.Email.String != "test@exemple.be" {
		t.Fatalf("réglages non appliqués : %+v", l)
	}
	if rec := GET(app, "/l/test-corrige"); rec.Code != 200 {
		t.Fatal("la page publique suit la nouvelle adresse")
	}
	app.db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat) VALUES ('prise','x','P','','')`)
	form.Set("slug", "prise")
	if rec := POST(app, "/e/edtest/reglages", form); rec.Code != 409 {
		t.Fatalf("adresse déjà prise → 409, reçu %d", rec.Code)
	}
	// la lettre s'enregistre sans toucher à l'e-mail
	POST(app, "/e/edtest", url.Values{"lettre": {"Bonjour"}, "etat": {""}})
	l, _ = app.listeParJetonEdition("edtest")
	if l.Email.String != "test@exemple.be" || l.Lettre != "Bonjour" {
		t.Fatal("enregistrer la lettre ne doit pas effacer l'e-mail")
	}
}

func TestDecision_UnePercheSeCorrigeEntierement(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(5), "KIKK, Namurr", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "anna@exemple.be")
	form := url.Values{"titre": {"KIKK, Namur"}, "date": {i.Quand.String[:10]}, "heure": {""},
		"lieu": {i.Lieu}, "description": {"journée du vendredi"}, "fin": {dans(6)}, "visibilite": {"lien"}, "jy_vais": {"0"}}
	if rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", i.ID), form); rec.Code != 303 {
		t.Fatalf("maj : %d", rec.Code)
	}
	j, _, _ := app.intentionParJeton(i.Jeton)
	if j.Titre != "KIKK, Namur" || j.Description != "journée du vendredi" || j.Fin.String != dans(6) || j.Visibilite != "lien" || !j.JyVais {
		t.Fatalf("correction non appliquée : %+v", j)
	}
	if len(m.Envois) != 1 {
		t.Fatalf("ajouter un dernier jour est de la logistique : un mot, reçu %d", len(m.Envois))
	}
	m.Envois = nil
	if rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", i.ID), form); rec.Code != 303 || len(m.Envois) != 0 {
		t.Fatal("corriger sans rien changer aux dates ni au lieu : personne n'est prévenu")
	}
	form.Set("lieu", "Namur, gare")
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", i.ID), form)
	if len(m.Envois) != 1 {
		t.Fatalf("changer le lieu prévient ceux qui ont un e-mail, envois : %d", len(m.Envois))
	}
}

func TestSecurite_RecuperationNeBloquePasLeService(t *testing.T) {
	app, m := appTest(t)
	app.db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat, email) VALUES ('a','ea','A','','','h@exemple.be')`)
	app.db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat, email) VALUES ('b','eb','B','','','h@exemple.be')`)
	fini := make(chan bool)
	go func() { POST(app, "/recuperation", url.Values{"email": {"h@exemple.be"}}); fini <- true }()
	select {
	case <-fini:
	case <-time.After(3 * time.Second):
		t.Fatal("la récupération bloque (lecture ouverte pendant une écriture sur l'unique connexion)")
	}
	if len(m.Envois) != 2 {
		t.Fatalf("un courriel par liste, reçu %d", len(m.Envois))
	}
	if rec := GET(app, "/l/a"); rec.Code != 200 {
		t.Fatal("le service doit répondre après la récupération")
	}
}

func TestSecurite_LEmailNEstPasUneCle(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	app.db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat, email) VALUES ('victime','secret-victime','Victime','','','v@exemple.be')`)
	// l'attaquant déclare l'e-mail de la victime dans ses réglages
	POST(app, "/e/edtest/reglages", url.Values{"titre": {"Attaquant"}, "slug": {"test"}, "email": {"v@exemple.be"}})
	if strings.Contains(GET(app, "/e/edtest").Body.String(), "secret-victime") {
		t.Fatal("un e-mail non vérifié ne doit jamais donner accès à l'atelier d'une autre liste")
	}
}

// ---- lot 1 : robustesse ----

func TestSecurite_CorpsBorne(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	gros := url.Values{"lettre": {strings.Repeat("a", 100_000)}}
	if rec := POST(app, "/e/edtest", gros); rec.Code != 413 {
		t.Fatalf("un corps de 100 Ko doit être refusé (413), reçu %d", rec.Code)
	}
	long := url.Values{"titre": {strings.Repeat("a", 121)}, "date": {dans(3)}}
	if rec := POST(app, "/e/edtest/intentions", long); rec.Code != 400 {
		t.Fatalf("un titre de 121 caractères doit être refusé (400), reçu %d", rec.Code)
	}
}

func TestSecurite_LesModificationsSontLimitees(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	form := url.Values{"titre": {"KIKK"}, "date": {dans(3)}}
	var dernier int
	for k := 0; k < 16; k++ {
		dernier = POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", i.ID), form).Code
	}
	if dernier != 429 {
		t.Fatalf("la 16e modification en une minute doit être freinée (429), reçu %d", dernier)
	}
}

func TestSecurite_UnEmailParPerche(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(1), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "anna@exemple.be")
	repondreTest(t, app, i.Jeton, "Anna", "peut_etre", "", "anna@exemple.be")
	repondreTest(t, app, i.Jeton, "Bot", "jy_serai", "", "pas-un-email")
	app.envoyerRappels()
	if len(m.Envois) != 1 {
		t.Fatalf("un seul rappel pour un même e-mail, reçu %d", len(m.Envois))
	}
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE email IS NOT NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("e-mail en double ou implausible non retenu, trouvé %d", n)
	}
}

func TestSecurite_PlafondDeReponses(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	for k := 0; k < plafondReponses; k++ {
		app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut) VALUES (?, 'x', 'jy_serai')`, i.ID)
	}
	if rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}}); rec.Code != 403 {
		t.Fatalf("au-delà du plafond, la réponse est refusée avec un mot (403), reçu %d", rec.Code)
	}
}

func TestSecurite_CodeExpireOuConsommeUneSeuleFois(t *testing.T) {
	app, _ := appTest(t)
	app.politique = "invitation"
	app.db.Exec(`INSERT INTO codes_invitation (code, cree_le) VALUES ('vieux', datetime('now', '-40 days'))`)
	form := url.Values{"titre": {"Vieux"}, "email": {"v@exemple.be"}, "code": {"vieux"}}
	if rec := POST(app, "/listes", form); rec.Code != 403 {
		t.Fatalf("un code de 40 jours est expiré, reçu %d", rec.Code)
	}
	app.db.Exec(`INSERT INTO codes_invitation (code) VALUES ('frais')`)
	form.Set("code", "frais")
	if rec := POST(app, "/listes", form); rec.Code != 303 {
		t.Fatalf("code frais : %d", rec.Code)
	}
	var listeID sql.NullInt64
	app.db.QueryRow(`SELECT liste_id FROM codes_invitation WHERE code = 'frais'`).Scan(&listeID)
	if !listeID.Valid {
		t.Fatal("le code consommé doit pointer vers la liste créée")
	}
}

func TestSecurite_XForwardedForNestCruQueDerriereUnProxy(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	frappe := func(k int) int {
		req := httptest.NewRequest("POST", "/e/edtest", strings.NewReader("lettre=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", k))
		rec := httptest.NewRecorder()
		app.handler().ServeHTTP(rec, req)
		return rec.Code
	}
	var dernier int
	for k := 0; k < 16; k++ {
		dernier = frappe(k)
	}
	if dernier != 429 {
		t.Fatalf("sans proxy déclaré, inventer des X-Forwarded-For ne contourne pas le limiteur, reçu %d", dernier)
	}
	app.derriereProxy, app.limiteur = true, nouveauLimiteur()
	for k := 0; k < 16; k++ {
		dernier = frappe(k)
	}
	if dernier != 303 {
		t.Fatalf("derrière un proxy, chaque adresse transmise compte pour elle-même, reçu %d", dernier)
	}
}

func TestSecurite_URLExterneFiltree(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	POST(app, "/e/edtest/intentions", url.Values{"tendre": {"1"}, "titre": {"Expo"}, "date": {dans(3)}, "url_externe": {"javascript:alert(1)"}})
	var u sql.NullString
	app.db.QueryRow(`SELECT url_externe FROM intentions WHERE titre = 'Expo'`).Scan(&u)
	if u.Valid {
		t.Fatalf("une URL qui n'est pas http(s) est ignorée, trouvé %q", u.String)
	}
}

func TestSecurite_EnTetes(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	h := GET(app, "/l/test").Header()
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "same-origin" {
		t.Fatal("en-têtes de sécurité absents")
	}
	csp := h.Get("Content-Security-Policy")
	if !strings.Contains(csp, "'sha256-") || strings.Contains(csp, "unsafe") {
		t.Fatalf("la CSP autorise le script par empreinte, rien d'autre : %q", csp)
	}
	// l'empreinte doit être celle du script réellement servi (html/template réécrit le JS)
	page := GET(app, "/l/test").Body.String()
	script := page[strings.Index(page, "<script>")+len("<script>") : strings.Index(page, "</script>")]
	somme := sha256.Sum256([]byte(script))
	if !strings.Contains(csp, "'sha256-"+base64.StdEncoding.EncodeToString(somme[:])+"'") {
		t.Fatal("l'empreinte de la CSP ne correspond pas au script servi : le bouton « copier » serait bloqué")
	}
	req := httptest.NewRequest("POST", "/oublier", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	app.handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("« oublier » depuis un autre site est refusé, reçu %d", rec.Code)
	}
}

// ---- lot 2 : le partage et la perche ----

func TestDecision_UneReponseParPrenom(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Léa", "jy_serai", "", "")
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"léa"}, "statut": {"peut_etre"}, "prenom_visible": {"1"}})
	if !strings.HasPrefix(rec.Header().Get("Location"), "/i/"+i.Jeton+"?") {
		t.Fatalf("après réponse, redirection vers la perche (rechargement sans doublon), reçu %q", rec.Header().Get("Location"))
	}
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE intention_id = ?`, i.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("revenir et redonner son prénom remplace la réponse ; trouvé %d lignes", n)
	}
	page := GET(app, rec.Header().Get("Location")).Body.String()
	if !strings.Contains(page, "Peut-être : léa") || strings.Contains(page, "Seront là") || !strings.Contains(page, "Réponse enregistrée") {
		t.Fatal("la page de la perche montre la réponse à jour et le mot de confirmation")
	}
}

func TestDecision_PlusDOptionAConfirmer(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	POST(app, "/e/edtest/intentions", url.Values{"tendre": {"1"}, "titre": {"Expo"}, "date": {dans(3)}, "jy_vais": {"0"}})
	var jy bool
	app.db.QueryRow(`SELECT jy_vais_de_toute_facon FROM intentions WHERE titre = 'Expo'`).Scan(&jy)
	if !jy {
		t.Fatal("« j'y vais de toute façon » n'est plus une option : c'est la règle")
	}
	if strings.Contains(GET(app, "/e/edtest").Body.String(), "à confirmer") {
		t.Fatal("l'atelier ne propose plus « à confirmer »")
	}
}

func TestDecision_CarteDePartageComplete(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	app.db.Exec(`UPDATE listes SET lettre = ? WHERE slug = 'test'`, "Bonjour,\nÇa fait un moment — quelques années, en fait. Voici ce que je compte faire ces prochaines semaines.")
	page := GET(app, "/l/test").Body.String()
	if strings.Contains(page, `og:description" content="Bonjour,"`) || !strings.Contains(page, `content="Ça fait un moment`) {
		t.Fatal("la carte saute la salutation et montre la première vraie phrase")
	}
	if !strings.Contains(page, "og:image") || !strings.Contains(page, `rel="icon"`) || !strings.Contains(page, `rel="alternate" type="application/json"`) {
		t.Fatal("image de carte, favicon et JSON en lien alternatif")
	}
	if strings.Contains(page, ">JSON<") {
		t.Fatal("« JSON » n'apparaît plus sur la page publique")
	}
}

func TestDecision_DatesLisibles(t *testing.T) {
	demain := quandFR(sql.NullString{String: dans(1) + "T18:30", Valid: true})
	if !strings.HasPrefix(demain, "demain, ") || !strings.HasSuffix(demain, " à 18h30") {
		t.Fatalf("demain : %q", demain)
	}
	if s := quandFR(sql.NullString{String: dans(4), Valid: true}); !strings.HasPrefix(s, "dans 4 jours — ") {
		t.Fatalf("dans 4 jours : %q", s)
	}
	if s := quandFR(sql.NullString{String: "2031-03-08", Valid: true}); s != "samedi 8 mars 2031" {
		t.Fatalf("année lointaine affichée : %q", s)
	}
}

func TestDecision_AnnuleeProprement(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "")
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/annuler", i.ID), nil)
	page := GET(app, "/i/"+i.Jeton).Body.String()
	if strings.Contains(page, "de toute façon") || strings.Contains(page, "Seront là") || strings.Contains(page, "calendrier") {
		t.Fatal("une perche annulée ne promet plus rien")
	}
	if ics := GET(app, "/i/"+i.Jeton+".ics").Body.String(); !strings.Contains(ics, "STATUS:CANCELLED") {
		t.Fatal("l'ICS d'une perche annulée le dit à l'agenda")
	}
}

func TestDecision_EmailsDesInvitesPurges(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	vieille := intentionTest(t, app, ilYA(40), "Vieille", "page")
	recente := intentionTest(t, app, dans(3), "Récente", "page")
	app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut, email) VALUES (?, 'Anna', 'jy_serai', 'a@exemple.be'), (?, 'Marc', 'jy_serai', 'm@exemple.be')`, vieille.ID, recente.ID)
	app.purgerEmails()
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE email IS NOT NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("un mois après la date, l'e-mail est effacé ; il en reste %d", n)
	}
	if strings.Contains(GET(app, "/e/edtest/export.json").Body.String(), "exemple.be") {
		t.Fatal("l'export de l'hôte ne contient pas les e-mails des invités")
	}
}

// ---- lot 3 : l'atelier et les textes ----

func TestDecision_ErreursDansLeGabarit(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	rec := POST(app, "/e/edtest/intentions", url.Values{"tendre": {"1"}, "titre": {"Expo"}})
	body := rec.Body.String()
	if rec.Code != 400 || !strings.Contains(body, "<main") || !strings.Contains(body, `href="/e/edtest"`) || strings.Contains(body, "variante") {
		t.Fatalf("une erreur est une page du site avec un retour à l'atelier, reçu %d : %s", rec.Code, body[:min(200, len(body))])
	}
	rec = GET(app, "/e/inconnu")
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "page not found") || !strings.Contains(rec.Body.String(), "édition") {
		t.Fatal("le 404 parle français et oriente vers l'e-mail")
	}
	for _, casse := range []string{"/l/Test.", "/l/test/", "/l/TEST"} {
		rec := GET(app, casse)
		if rec.Code == 200 {
			continue
		}
		if rec.Code != 301 || GET(app, rec.Header().Get("Location")).Code != 200 {
			t.Fatalf("lien abîmé %q non réparé : %d → %s", casse, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestDecision_ConfirmerAvantAnnulerEtEffacer_PuisRetablir(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "belle idée", "anna@exemple.be")
	var idRep int64
	app.db.QueryRow(`SELECT id FROM reponses`).Scan(&idRep)
	if page := GET(app, fmt.Sprintf("/e/edtest/reponses/%d/effacer", idRep)).Body.String(); !strings.Contains(page, "Effacer la réponse de Anna") || !strings.Contains(page, "belle idée") {
		t.Fatal("effacer passe par une page qui nomme la personne et son mot")
	}
	if page := GET(app, fmt.Sprintf("/e/edtest/intentions/%d/annuler", i.ID)).Body.String(); !strings.Contains(page, "Retirer « KIKK » de ta page ?") || !strings.Contains(page, "sera prévenue") {
		t.Fatal("annuler passe par une page qui dit qui sera prévenu")
	}
	atelier := GET(app, "/e/edtest").Body.String()
	if strings.Contains(atelier, `action="/e/edtest/reponses/`) {
		t.Fatal("plus de bouton « effacer » à un clic dans l'atelier")
	}
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/annuler", i.ID), nil)
	if len(m.Envois) != 1 {
		t.Fatalf("annulation : un envoi, reçu %d", len(m.Envois))
	}
	rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/retablir", i.ID), nil)
	if rec.Code != 303 || len(m.Envois) != 2 {
		t.Fatalf("rétablir : %d, envois %d", rec.Code, len(m.Envois))
	}
	j, _, _ := app.intentionParJeton(i.Jeton)
	if j.AnnuleeLe.Valid {
		t.Fatal("la perche est rétablie")
	}
	if !strings.Contains(GET(app, "/i/"+i.Jeton).Body.String(), `name="statut"`) {
		t.Fatal("une perche rétablie reprend les réponses")
	}
}

func TestDecision_FermerMaListe(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(1), "KIKK", "page")
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "anna@exemple.be")
	if !strings.Contains(GET(app, "/e/edtest/fermer").Body.String(), "fermée") {
		t.Fatal("fermer passe par une confirmation")
	}
	if rec := POST(app, "/e/edtest/fermer", nil); rec.Code != 303 {
		t.Fatalf("fermer : %d", rec.Code)
	}
	page := GET(app, "/l/test").Body.String()
	if !strings.Contains(page, "fermée pour le moment") || strings.Contains(page, "KIKK") || strings.Contains(page, "envies du moment") {
		t.Fatal("la page publique ne montre plus que « fermée pour le moment »")
	}
	if rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Marc"}, "statut": {"jy_serai"}}); rec.Code != 410 {
		t.Fatalf("une perche d'une liste en retrait ne prend plus de réponse, reçu %d", rec.Code)
	}
	app.envoyerRappels()
	if len(m.Envois) != 0 {
		t.Fatal("pas de rappel pour une liste en retrait")
	}
	if !strings.Contains(GET(app, "/e/edtest").Body.String(), "KIKK") {
		t.Fatal("l'atelier garde tout")
	}
	POST(app, "/e/edtest/rouvrir", nil)
	if !strings.Contains(GET(app, "/l/test").Body.String(), "KIKK") {
		t.Fatal("rouvrir rend la page")
	}
}

func TestDecision_PasDeDoublonEnInstanceOuverte(t *testing.T) {
	app, m := appTest(t)
	app.politique = "ouverte"
	POST(app, "/listes", url.Values{"titre": {"Les perches d'Anna"}, "email": {"anna@exemple.be"}})
	rec := POST(app, "/listes", url.Values{"titre": {"Anna, encore"}, "email": {"anna@exemple.be"}})
	var n int
	app.db.QueryRow(`SELECT count(*) FROM listes`).Scan(&n)
	if n != 1 || rec.Code != 200 || !strings.Contains(rec.Body.String(), "a déjà sa liste") {
		t.Fatalf("un e-mail qui a déjà sa liste ne crée pas de doublon : listes %d, code %d", n, rec.Code)
	}
	if len(m.Envois) != 2 { // bienvenue + renvoi
		t.Fatalf("le lien de l'atelier est renvoyé, envois %d", len(m.Envois))
	}
}

func TestDecision_CodeInvalideDitAuGet(t *testing.T) {
	app, _ := appTest(t)
	app.politique = "invitation"
	page := GET(app, "/?code=faux").Body.String()
	if strings.Contains(page, `name="titre"`) || !strings.Contains(page, "a déjà servi") {
		t.Fatal("un lien d'invitation faux ne montre pas le formulaire, et le dit")
	}
}

func TestDecision_AtelierStable(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	rec := POST(app, "/e/edtest", url.Values{"lettre": {"Bonjour"}})
	if rec.Header().Get("Location") != "/e/edtest?ok=lettre#lettre" {
		t.Fatalf("enregistrer l'introduction ramène à l'introduction, reçu %q", rec.Header().Get("Location"))
	}
	if page := GET(app, "/e/edtest?ok=lettre").Body.String(); !strings.Contains(page, "Enregistré") || strings.Index(page, `id="lettre"`) > strings.Index(page, `id="perches"`) {
		t.Fatal("l'introduction est confirmée et reste en tête de l'atelier")
	}
	rec = POST(app, "/e/edtest/intentions", url.Values{"tendre": {"1"}, "titre": {"Expo"}, "date": {dans(3)}})
	if !strings.Contains(rec.Header().Get("Location"), "?ok=ajoute#perche-") {
		t.Fatalf("ajouter ramène à l'événement ajouté, reçu %q", rec.Header().Get("Location"))
	}
}

func TestDecision_MarkdownDeLettre(t *testing.T) {
	cas := map[string]string{
		"Bonjour,\nça va **bien**.":              "<p>Bonjour,<br>ça va <strong>bien</strong>.</p>",
		"- expo\n- ciné":                         "<ul><li>expo</li><li>ciné</li></ul>",
		"voir [le site](https://kikk.be) !":      `<p>voir <a href="https://kikk.be" rel="noopener">le site</a> !</p>`,
		"<script>alert(1)</script> *doux*":       "<p>&lt;script&gt;alert(1)&lt;/script&gt; <em>doux</em></p>",
		"[x](javascript:alert(1))":               "<p>[x](javascript:alert(1))</p>",
		"https://perches.example/l/gilles voilà": `<p><a href="https://perches.example/l/gilles" rel="noopener">https://perches.example/l/gilles</a> voilà</p>`,
	}
	for src, attendu := range cas {
		if got := strings.TrimSpace(string(rendreMarkdown(src))); got != attendu {
			t.Errorf("md(%q)\n  = %s\n  ≠ %s", src, got, attendu)
		}
	}
	if s := sansMarkdown("**Bonjour** [ami](https://x.y)"); s != "Bonjour ami" {
		t.Errorf("sansMarkdown : %q", s)
	}
	app, _ := appTest(t)
	listeTest(t, app)
	app.db.Exec(`UPDATE listes SET lettre = '**Salut** à tous' WHERE slug = 'test'`)
	if page := GET(app, "/l/test").Body.String(); !strings.Contains(page, "<strong>Salut</strong>") {
		t.Fatal("la page rend le Markdown de l'introduction")
	}
}

// ---- hors périmètre : le test d'absence ----

func TestHorsPerimetre_LeSchemaResteMaigre(t *testing.T) {
	app, _ := appTest(t)
	attendu := map[string]bool{
		"listes": true, "intentions": true, "reponses": true, "disponibilites": true,
		"liens_listes": true, "codes_invitation": true, "envois": true,
	}
	rows, err := app.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	vues := map[string]bool{}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		vues[n] = true
		if !attendu[n] {
			t.Errorf("table inattendue : %s — élargir le schéma est une décision de produit, pas de code", n)
		}
	}
	for n := range attendu {
		if !vues[n] {
			t.Errorf("table manquante : %s", n)
		}
	}
}

func TestDecision_PercheSurPlusieursJours(t *testing.T) {
	// Premier retour d'usage (Arles, 28 septembre → 2 octobre) : une perche peut durer.
	app, _ := appTest(t)
	listeTest(t, app)
	rec := POST(app, "/e/edtest/intentions", url.Values{"tendre": {"1"}, "titre": {"Arles"}, "date": {ilYA(1)}, "fin": {dans(2)}})
	if rec.Code != 303 {
		t.Fatalf("création avec une fin : %d %s", rec.Code, rec.Body.String())
	}
	var j string
	app.db.QueryRow(`SELECT jeton FROM intentions WHERE titre = 'Arles'`).Scan(&j)
	page := GET(app, "/i/"+j).Body.String()
	if !strings.Contains(page, "du ") || !strings.Contains(page, " au ") || !strings.Contains(page, `name="statut"`) {
		t.Fatal("une perche en cours se dit « du … au … » et prend encore des réponses")
	}
	if !strings.Contains(GET(app, "/l/test").Body.String(), "Arles") {
		t.Fatal("commencée hier, finie après-demain : la perche est encore sur la page")
	}
	ics := GET(app, "/i/"+j+".ics").Body.String()
	if !strings.Contains(ics, "DTSTART;VALUE=DATE:") || !strings.Contains(ics, "DTEND;VALUE=DATE:") {
		t.Fatalf("l'agenda reçoit les journées entières, du premier au dernier jour : %s", ics)
	}
	if rec := POST(app, "/e/edtest/intentions", url.Values{"titre": {"À l'envers"}, "date": {dans(5)}, "fin": {dans(2)}}); rec.Code != 400 {
		t.Fatalf("une fin avant le début est refusée, reçu %d", rec.Code)
	}
	i := intentionTest(t, app, ilYA(3), "Finie", "page")
	app.db.Exec(`UPDATE intentions SET fin = ? WHERE id = ?`, ilYA(1), i.ID)
	if strings.Contains(GET(app, "/l/test").Body.String(), "Finie") {
		t.Fatal("après son dernier jour, la perche quitte la page")
	}
}

func TestDecision_RepondreDepuisLaListe(t *testing.T) {
	// Lot B2 : la perche s'ouvre sur place, dans la liste ; on y répond sans changer de page,
	// et l'on revient sur la liste, carte ouverte, avec le mot de confirmation dedans.
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	page := GET(app, "/l/test").Body.String()
	if !strings.Contains(page, `name="statut"`) || !strings.Contains(page, `name="retour"`) || !strings.Contains(page, "machines ensemble") {
		t.Fatal("la liste porte la perche entière et son formulaire")
	}
	if strings.Index(page, "voici mes envies") > strings.Index(page, "KIKK") {
		t.Fatal("la lettre précède toujours les perches")
	}
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}, "prenom_visible": {"1"}, "retour": {"liste"}})
	loc := rec.Header().Get("Location")
	if rec.Code != 303 || !strings.HasPrefix(loc, "/l/test?") || !strings.HasSuffix(loc, "#p-"+i.Jeton) {
		t.Fatalf("depuis la liste, on revient à la liste : %d %q", rec.Code, loc)
	}
	page = GET(app, loc).Body.String()
	if !strings.Contains(page, "Réponse enregistrée") || !strings.Contains(page, `id="p-`+i.Jeton+`" open`) || !strings.Contains(page, "Seront là : Anna") {
		t.Fatal("la carte répondue est ouverte, avec la confirmation et la présence")
	}
	if !strings.Contains(GET(app, "/i/"+i.Jeton).Body.String(), "Seront là : Anna") {
		t.Fatal("la page de la perche montre la même chose")
	}
}

func TestDecision_MenuEtTheme(t *testing.T) {
	app, _ := appTest(t)
	app.politique = "invitation"
	listeTest(t, app)
	page := GET(app, "/e/edtest").Body.String()
	for _, lien := range []string{`href="/e/edtest/reglages"`, `href="/e/edtest/inviter"`, `href="/a-propos"`, `action="/theme"`, `action="/oublier"`} {
		if !strings.Contains(page, lien) {
			t.Fatalf("le menu de l'édition manque : %s", lien)
		}
	}
	if strings.Contains(page, `name="slug"`) {
		t.Fatal("les réglages ne sont plus dans le flux de l'édition")
	}
	if rec := GET(app, "/e/edtest/reglages"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `name="slug"`) {
		t.Fatalf("page réglages : %d", rec.Code)
	}
	if rec := GET(app, "/e/inconnu/reglages"); rec.Code != 404 {
		t.Fatalf("réglages d'une édition inconnue : %d", rec.Code)
	}
	if rec := GET(app, "/a-propos"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "AGPL") || !strings.Contains(rec.Body.String(), "github.com") {
		t.Fatalf("à propos : %d", rec.Code)
	}
	rec := POST(app, "/theme", url.Values{"theme": {"sombre"}, "retour": {"/e/edtest"}})
	cookie := rec.Header().Get("Set-Cookie")
	if rec.Code != 303 || rec.Header().Get("Location") != "/e/edtest" || !strings.HasPrefix(cookie, "theme=sombre") {
		t.Fatalf("choisir un thème pose un cookie et ramène à l'édition : %d %q %q", rec.Code, rec.Header().Get("Location"), cookie)
	}
	req := httptest.NewRequest("GET", "/l/test", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "sombre"})
	w := httptest.NewRecorder()
	app.handler().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `data-theme="dark"`) {
		t.Fatal("le thème choisi s'applique à toutes les pages")
	}
	if strings.Contains(GET(app, "/l/test").Body.String(), "data-theme") {
		t.Fatal("sans choix, le système décide")
	}
	if rec := POST(app, "/theme", url.Values{"theme": {"auto"}, "retour": {"//ailleurs"}}); rec.Header().Get("Location") != "/" || !strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("« auto » efface le cookie ; un retour hors du site est ignoré")
	}
}

// ---- C12 : tout est repéré ; la perche est un geste posé dessus (lots E et F, 2026-08-28) ----

func TestConv12_RepereSansReponse(t *testing.T) {
	app, m := appTest(t)
	listeTest(t, app)
	// Repérer sans tendre la perche : sur la page, sans formulaire, sans rappel, refus d'une réponse.
	rec := POST(app, "/e/edtest/intentions", url.Values{"titre": {"Expo Ensor"}, "date": {dans(1)}, "fin": {dans(60)}})
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "?ok=ajoute#perche-") {
		t.Fatalf("repérer : %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var j string
	var id int64
	app.db.QueryRow(`SELECT jeton, id FROM intentions WHERE titre = 'Expo Ensor'`).Scan(&j, &id)
	page := GET(app, "/l/test").Body.String()
	if !strings.Contains(page, "Expo Ensor") || strings.Contains(page, `name="statut"`) || !strings.Contains(page, "sans engagement") || strings.Contains(page, `class="signe"`) {
		t.Fatal("un repéré est sur la page, sans formulaire ni signe de perche")
	}
	if strings.Contains(page, `class="filtre"`) {
		t.Fatal("sans perche tendue, pas de filtre")
	}
	if p := GET(app, "/i/"+j).Body.String(); strings.Contains(p, `name="statut"`) || !strings.Contains(p, "sans engagement") {
		t.Fatal("la page d'un repéré n'a pas de formulaire")
	}
	if rec := POST(app, "/i/"+j+"/reponses", url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}}); rec.Code != 410 {
		t.Fatalf("répondre à un repéré est refusé (410), reçu %d", rec.Code)
	}
	app.envoyerRappels()
	if len(m.Envois) != 0 {
		t.Fatal("aucun rappel pour un repéré")
	}
	if strings.Contains(GET(app, "/e/edtest/export.json").Body.String(), `"perche_tendue":`) {
		t.Fatal("l'export d'un repéré ne porte pas de perche")
	}
	// Tendre la perche — avec ses propres dates, dans l'événement.
	if rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/perche", id), url.Values{"perche_date": {dans(1)}}); rec.Code != 303 {
		t.Fatalf("tendre la perche : %d", rec.Code)
	}
	page = GET(app, "/l/test").Body.String()
	if !strings.Contains(page, `name="statut"`) || !strings.Contains(page, `class="signe"`) || !strings.Contains(page, `class="filtre"`) {
		t.Fatal("perche tendue : formulaire, signe et filtre")
	}
	if p := GET(app, "/l/test?perches").Body.String(); !strings.Contains(p, "Expo Ensor") {
		t.Fatal("le filtre « Perches » garde les perches tendues")
	}
	repondreTest(t, app, j, "Anna", "jy_serai", "", "anna@exemple.be")
	app.envoyerRappels()
	if len(m.Envois) != 1 || !strings.Contains(m.Envois[0].Sujet, "demain") {
		t.Fatalf("le rappel de la veille suit la date de la perche, envois : %+v", m.Envois)
	}
	// Retirer la perche : l'événement reste repéré, ceux qui ont un e-mail sont prévenus, la réponse reste dans l'édition.
	if !strings.Contains(GET(app, fmt.Sprintf("/e/edtest/intentions/%d/retirer-perche", id)).Body.String(), "sera prévenue") {
		t.Fatal("retirer la perche passe par une confirmation qui dit qui est prévenu")
	}
	m.Envois = nil
	if rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/retirer-perche", id), nil); rec.Code != 303 || len(m.Envois) != 1 {
		t.Fatalf("retirer la perche : %d, envois %d", rec.Code, len(m.Envois))
	}
	page = GET(app, "/l/test").Body.String()
	if !strings.Contains(page, "Expo Ensor") || strings.Contains(page, `name="statut"`) || !strings.Contains(page, "sans engagement") {
		t.Fatal("perche retirée : l'événement reste, sans formulaire")
	}
	if !strings.Contains(GET(app, "/e/edtest").Body.String(), "Anna") {
		t.Fatal("l'hôte garde la réponse reçue")
	}
}

func TestDecision_PercheAvecSesPropresDates(t *testing.T) {
	// Lot F : l'événement a ses dates, la perche les siennes — l'invité ne voit que celles de l'hôte.
	app, m := appTest(t)
	listeTest(t, app)
	rec := POST(app, "/e/edtest/intentions", url.Values{"titre": {"KIKK"}, "date": {dans(1)}, "fin": {dans(5)},
		"tendre": {"1"}, "perche_date": {dans(3)}, "perche_heure": {"14:00"}})
	if rec.Code != 303 {
		t.Fatalf("repérer et tendre : %d %s", rec.Code, rec.Body.String())
	}
	var j string
	app.db.QueryRow(`SELECT jeton FROM intentions WHERE titre = 'KIKK'`).Scan(&j)
	i, _, _ := app.intentionParJeton(j)
	if i.PercheQuand.String != dans(3)+"T14:00" || i.PercheFin.Valid || i.Fin.String != dans(5) {
		t.Fatalf("dates de la perche et de l'événement : %+v", i)
	}
	page := GET(app, "/i/"+j).Body.String()
	if !strings.Contains(page, "du ") || !strings.Contains(page, "J'y vais ") || !strings.Contains(page, "à 14h00") {
		t.Fatal("la page dit les dates de l'événement, puis quand l'hôte y va")
	}
	if !strings.Contains(GET(app, "/l/test").Body.String(), i.JourCourt()) || strings.Contains(GET(app, "/l/test").Body.String(), "→ ") {
		t.Fatal("la chronologie montre la date de la perche, pas la période de l'événement")
	}
	if ics := GET(app, "/i/"+j+".ics").Body.String(); !strings.Contains(ics, "DTSTART:"+strings.ReplaceAll(dans(3), "-", "")+"T140000") {
		t.Fatalf("l'agenda reçoit la date de la perche : %s", ics)
	}
	repondreTest(t, app, j, "Anna", "jy_serai", "", "anna@exemple.be")
	app.envoyerRappels()
	if len(m.Envois) != 0 {
		t.Fatal("pas de rappel : la perche est dans trois jours, pas demain")
	}
	// Changer les dates de la perche prévient ; le jour de l'événement ne bouge pas.
	m.Envois = nil
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/perche", i.ID), url.Values{"perche_date": {dans(4)}})
	i2, _, _ := app.intentionParJeton(j)
	if len(m.Envois) != 1 || i2.PercheQuand.String != dans(4) || i2.Quand.String != dans(1) {
		t.Fatalf("changer quand j'y vais : envois %d, perche %q, événement %q", len(m.Envois), i2.PercheQuand.String, i2.Quand.String)
	}
}

func TestDecision_ToutDuLongSuitLEvenement(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	// tendue sans dates propres : suit l'événement quand il bouge
	POST(app, "/e/edtest/intentions", url.Values{"titre": {"Marché"}, "date": {dans(3)}, "tendre": {"1"}})
	var id int64
	app.db.QueryRow(`SELECT id FROM intentions WHERE titre = 'Marché'`).Scan(&id)
	i, _ := app.intentionDeListe(1, id)
	if i.PercheQuand.Valid || !i.PercheAuxDatesDeLEvenement() {
		t.Fatalf("tout du long = pas de dates propres : %+v", i)
	}
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", id), url.Values{"titre": {"Marché"}, "date": {dans(4)}})
	if q, _ := (func() (sql.NullString, sql.NullString) { i, _ := app.intentionDeListe(1, id); return i.DatesPerche() })(); q.String != dans(4) {
		t.Fatalf("la perche tout du long suit l'événement : %q", q.String)
	}
	// des dates propres (migrées ou saisies) restent quand l'événement est corrigé — le cas d'Arles
	app.db.Exec(`UPDATE intentions SET perche_quand = ?, perche_fin = ? WHERE id = ?`, dans(4), dans(6), id)
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", id), url.Values{"titre": {"Marché"}, "date": {dans(1)}, "fin": {dans(9)}})
	i, _ = app.intentionDeListe(1, id)
	q, f := i.DatesPerche()
	if q.String != dans(4) || f.String != dans(6) || i.Quand.String != dans(1) || i.PercheAuxDatesDeLEvenement() {
		t.Fatalf("des dates propres ne bougent pas : perche %q→%q, événement %q→%q", q.String, f.String, i.Quand.String, i.Fin.String)
	}
}

func TestDecision_BarreDHoteSurLesPagesPubliques(t *testing.T) {
	// Le navigateur de l'hôte (cookie « atelier ») voit, sur sa page et ses perches, des liens vers
	// l'édition ouverte sur le bon événement. Personne d'autre ne voit le moindre lien /e/.
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	r := repereTest(t, app, dans(5), "Expo")
	for _, chemin := range []string{"/l/test", "/i/" + i.Jeton, "/i/" + r.Jeton} {
		if strings.Contains(GET(app, chemin).Body.String(), "/e/") {
			t.Fatalf("%s : un lecteur sans cookie ne doit jamais voir de lien d'édition", chemin)
		}
	}
	hote := func(chemin string) string {
		req := httptest.NewRequest("GET", chemin, nil)
		req.AddCookie(&http.Cookie{Name: "atelier", Value: "edtest"})
		w := httptest.NewRecorder()
		app.handler().ServeHTTP(w, req)
		return w.Body.String()
	}
	page := hote("/l/test")
	if !strings.Contains(page, `class="hote"`) || !strings.Contains(page, fmt.Sprintf(`href="/e/edtest?modifier=%d#perche-%d"`, i.ID, i.ID)) || !strings.Contains(page, fmt.Sprintf(`href="/e/edtest?tendre=%d#perche-%d"`, r.ID, r.ID)) {
		t.Fatal("l'hôte voit la barre et, par carte, « Modifier » — et « Tendre la perche » sur un repéré")
	}
	if strings.Contains(hote("/i/"+i.Jeton), "Tendre la perche") || !strings.Contains(hote("/i/"+r.Jeton), "Tendre la perche") {
		t.Fatal("« Tendre la perche » seulement sur un repéré")
	}
	autre := httptest.NewRequest("GET", "/l/test", nil)
	autre.AddCookie(&http.Cookie{Name: "atelier", Value: "un-autre-jeton"})
	w := httptest.NewRecorder()
	app.handler().ServeHTTP(w, autre)
	if strings.Contains(w.Body.String(), "/e/") {
		t.Fatal("le cookie d'une autre liste ne donne rien ici")
	}
	edition := GET(app, fmt.Sprintf("/e/edtest?modifier=%d", i.ID)).Body.String()
	if !strings.Contains(edition, `<details open>
<summary>Modifier l'événement</summary>`) {
		t.Fatal("l'édition ouvre le formulaire demandé")
	}
}

func TestDecision_CarteDePartageParListeEtParEvenement(t *testing.T) {
	// L'image d'aperçu est dessinée pour chaque liste et chaque événement — 1200 × 630, PNG —
	// et son adresse porte une empreinte du contenu, pour que les messageries la rafraîchissent.
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page")
	for _, chemin := range []string{"/l/test.png", "/i/" + i.Jeton + ".png"} {
		rec := GET(app, chemin)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s : %d %s", chemin, rec.Code, rec.Header().Get("Content-Type"))
		}
		img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
		if err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
			t.Fatalf("%s : image invalide (%v)", chemin, err)
		}
	}
	page := GET(app, "/l/test").Body.String()
	if !strings.Contains(page, `property="og:image" content="http://perches.test/l/test.png?v=`) || !strings.Contains(page, `name="twitter:card"`) {
		t.Fatal("la page déclare sa carte, avec une empreinte, et la carte Twitter")
	}
	avant := regexp.MustCompile(`og:image" content="([^"]+)"`).FindStringSubmatch(GET(app, "/i/"+i.Jeton).Body.String())[1]
	POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", i.ID), url.Values{"titre": {"KIKK 2026"}, "date": {dans(3)}})
	apres := regexp.MustCompile(`og:image" content="([^"]+)"`).FindStringSubmatch(GET(app, "/i/"+i.Jeton).Body.String())[1]
	if avant == apres {
		t.Fatal("changer le titre change l'adresse de la carte")
	}
	if rec := GET(app, "/l/inconnue.png"); rec.Code != 404 {
		t.Fatalf("carte d'une liste inconnue : %d", rec.Code)
	}
}
