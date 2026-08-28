package main

// Suite de tests des conventions (docs/tests-conventions.md).
// Cette suite est un outil de gouvernance : une PR qui modifie un de ces tests
// est une PR qui demande à changer le produit.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func intentionTest(t *testing.T, app *App, quand, titre, visibilite string, capacite any) *Intention {
	t.Helper()
	compteurJeton++
	j := fmt.Sprintf("jetontest%d", compteurJeton)
	_, err := app.db.Exec(`INSERT INTO intentions (liste_id, jeton, titre, description, quand, lieu, capacite, visibilite)
		VALUES (1, ?, ?, 'On ira voir les machines ensemble.', ?, 'Namur', ?, ?)`,
		j, titre, quand, capacite, visibilite)
	if err != nil {
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
	i := intentionTest(t, app, dans(1), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Marc"}, "statut": {"non"}})
	if rec.Code != 400 {
		t.Fatalf("statut=non doit être rejeté (400), reçu %d", rec.Code)
	}
}

func TestConv02_SchemaRefuseLeNon(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
	_, err := app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut) VALUES (?, 'Marc', 'non')`, i.ID)
	if err == nil {
		t.Fatal("le schéma a accepté un statut « non »")
	}
}

func TestConv02_CapacitePleineNeBloquePas(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "Table au restaurant", "page", 2)
	for _, prenom := range []string{"Anna", "Marc", "Léa"} {
		repondreTest(t, app, i.Jeton, prenom, "jy_serai", "", "")
	}
	body := GET(app, "/i/"+i.Jeton).Body.String()
	if !strings.Contains(body, `name="statut"`) {
		t.Fatal("le formulaire a disparu une fois la capacité dépassée")
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "quel plaisir", "")
	if strings.Contains(strings.ToLower(GET(app, "/e/edtest").Body.String()), "répondre") {
		t.Fatal("la vue hôte suggère de répondre au mot — le mot se lit, sans suite")
	}
}

// ---- C4 : capacité indicative ----

func TestConv04_CapaciteAffichee(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "Table au restaurant", "page", 4)
	if !strings.Contains(GET(app, "/i/"+i.Jeton).Body.String(), "Capacité indicative : 4") {
		t.Fatal("la capacité définie n'est pas affichée")
	}
}

// ---- C5 : « j'y vais de toute façon » par défaut ----

func TestConv05_DefautSchemaEtAffichage(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	rec := POST(app, "/e/edtest/intentions", url.Values{"titre": {"Expo"}, "date": {dans(3)}})
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "on se réjouit", "anna@exemple.be")
	if len(m.Envois) != 0 {
		t.Fatalf("une réponse a déclenché %d envoi(s) — l'hôte va chercher l'information quand il veut", len(m.Envois))
	}
}

// ---- C8 : l'invité n'a jamais de compte ----

func TestConv08_ReponseSansCookieNiSession(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	intentionTest(t, app, ilYA(2), "Vernissage", "page", nil)
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
	i := intentionTest(t, app, ilYA(2), "Vernissage", "page", nil)
	app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut, mot) VALUES (?, 'Anna', 'jy_serai', 'belle soirée')`, i.ID)
	body := GET(app, "/e/edtest").Body.String()
	if !strings.Contains(body, "Anna") || !strings.Contains(body, "belle soirée") {
		t.Fatal("l'hôte doit garder ses lettres : prénoms et mots dans l'archive")
	}
}

func TestConv09_ReponsesPubliquesEffaceesApresDelai(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	i := intentionTest(t, app, ilYA(40), "Vernissage", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "Expo discrète", "lien", nil)
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
	i := intentionTest(t, app, dans(3), "Expo discrète", "lien", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
	rec := POST(app, "/i/"+i.Jeton+"/reponses", url.Values{"prenom": {"Anna"}, "statut": {"jy_serai"}})
	rec = GET(app, rec.Header().Get("Location"))
	if !strings.Contains(strings.ToLower(rec.Body.String()), "revérifie la page avant d'y aller") {
		t.Fatal("sans e-mail, la page doit dire de revérifier avant d'y aller")
	}
}

func TestDecision_ExportsToujoursDisponibles(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(1), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	code := strings.TrimSuffix(strings.TrimPrefix(loc, "/e/edtest?invitation="), "#inviter")
	if rec.Code != 303 || len(code) != 16 {
		t.Fatalf("redirection inattendue : %d %q", rec.Code, loc)
	}
	if !strings.Contains(GET(app, loc).Body.String(), "/?code="+code) {
		t.Fatal("l'atelier montre le lien d'invitation à envoyer")
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
	i := intentionTest(t, app, dans(5), "KIKK, Namurr", "page", nil)
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "anna@exemple.be")
	form := url.Values{"titre": {"KIKK, Namur"}, "date": {i.Quand.String[:10]}, "heure": {""},
		"lieu": {i.Lieu}, "description": {"journée du vendredi"}, "capacite": {"4"}, "visibilite": {"lien"}, "jy_vais": {"0"}}
	if rec := POST(app, fmt.Sprintf("/e/edtest/intentions/%d/maj", i.ID), form); rec.Code != 303 {
		t.Fatalf("maj : %d", rec.Code)
	}
	j, _, _ := app.intentionParJeton(i.Jeton)
	if j.Titre != "KIKK, Namur" || j.Description != "journée du vendredi" || j.Capacite.Int64 != 4 || j.Visibilite != "lien" || !j.JyVais {
		t.Fatalf("correction non appliquée : %+v", j)
	}
	if len(m.Envois) != 0 {
		t.Fatal("corriger une faute de frappe n'est pas de la logistique : personne n'est prévenu")
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(1), "KIKK", "page", nil)
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	POST(app, "/e/edtest/intentions", url.Values{"titre": {"Expo"}, "date": {dans(3)}, "url_externe": {"javascript:alert(1)"}})
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	if !strings.Contains(page, "Peut-être : léa") || strings.Contains(page, "Seront là") || !strings.Contains(page, "C'est noté") {
		t.Fatal("la page de la perche montre la réponse à jour et le mot de confirmation")
	}
}

func TestDecision_PlusDOptionAConfirmer(t *testing.T) {
	app, _ := appTest(t)
	listeTest(t, app)
	POST(app, "/e/edtest/intentions", url.Values{"titre": {"Expo"}, "date": {dans(3)}, "jy_vais": {"0"}})
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
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
	vieille := intentionTest(t, app, ilYA(40), "Vieille", "page", nil)
	recente := intentionTest(t, app, dans(3), "Récente", "page", nil)
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
	rec := POST(app, "/e/edtest/intentions", url.Values{"titre": {"Expo"}})
	body := rec.Body.String()
	if rec.Code != 400 || !strings.Contains(body, "<main") || !strings.Contains(body, `href="/e/edtest"`) || strings.Contains(body, "variante") {
		t.Fatalf("une erreur est une page du site avec un retour à l'atelier, reçu %d : %s", rec.Code, body[:min(200, len(body))])
	}
	rec = GET(app, "/e/inconnu")
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "page not found") || !strings.Contains(rec.Body.String(), "atelier") {
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
	i := intentionTest(t, app, dans(3), "KIKK", "page", nil)
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "belle idée", "anna@exemple.be")
	var idRep int64
	app.db.QueryRow(`SELECT id FROM reponses`).Scan(&idRep)
	if page := GET(app, fmt.Sprintf("/e/edtest/reponses/%d/effacer", idRep)).Body.String(); !strings.Contains(page, "Effacer la réponse de Anna") || !strings.Contains(page, "belle idée") {
		t.Fatal("effacer passe par une page qui nomme la personne et son mot")
	}
	if page := GET(app, fmt.Sprintf("/e/edtest/intentions/%d/annuler", i.ID)).Body.String(); !strings.Contains(page, "Annuler « KIKK » ?") || !strings.Contains(page, "sera prévenue") {
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
	i := intentionTest(t, app, dans(1), "KIKK", "page", nil)
	repondreTest(t, app, i.Jeton, "Anna", "jy_serai", "", "anna@exemple.be")
	if !strings.Contains(GET(app, "/e/edtest/fermer").Body.String(), "en retrait") {
		t.Fatal("fermer passe par une confirmation")
	}
	if rec := POST(app, "/e/edtest/fermer", nil); rec.Code != 303 {
		t.Fatalf("fermer : %d", rec.Code)
	}
	page := GET(app, "/l/test").Body.String()
	if !strings.Contains(page, "En retrait pour l'instant") || strings.Contains(page, "KIKK") || strings.Contains(page, "envies du moment") {
		t.Fatal("la page publique ne montre plus que « en retrait pour l'instant »")
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
	rec = POST(app, "/e/edtest/intentions", url.Values{"titre": {"Expo"}, "date": {dans(3)}})
	if !strings.HasSuffix(rec.Header().Get("Location"), "?ok=perche#perches") {
		t.Fatalf("tendre une perche ramène aux perches, reçu %q", rec.Header().Get("Location"))
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
