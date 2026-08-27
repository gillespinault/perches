package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var slugValide = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)

func (app *App) rendre(w http.ResponseWriter, nom string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := app.tpl.ExecuteTemplate(w, nom, data); err != nil {
		log.Printf("template %s : %v", nom, err)
	}
}

func ipDe(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	}
	hote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return hote
}

func (app *App) tropVite(w http.ResponseWriter, r *http.Request) bool {
	if app.limiteur.Autorise(ipDe(r)) {
		return false
	}
	http.Error(w, "Doucement — réessaie dans une minute.", http.StatusTooManyRequests)
	return true
}

func nullSi(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}

// envoyer passe par le mailer puis journalise dans `envois` (idempotence, audit).
func (app *App) envoyer(dest, sujet, corps, typ string, reponseID, listeID int64) {
	if err := app.mailer.Envoyer(dest, sujet, corps); err != nil {
		log.Printf("envoi %s : %v", typ, err)
		return
	}
	if _, err := app.db.Exec(`INSERT INTO envois (reponse_id, liste_id, type) VALUES (?,?,?)`,
		nullInt(reponseID), nullInt(listeID), typ); err != nil {
		log.Printf("journal envois : %v", err)
	}
}

// ---- accueil et création de liste ----

func (app *App) accueil(w http.ResponseWriter, r *http.Request) {
	app.rendre(w, "accueil.html", map[string]any{
		"TitrePage": "Perches",
		"Politique": app.politique,
	})
}

func (app *App) creerListe(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	if app.politique == "fermee" {
		http.Error(w, "Cette instance n'accepte pas de nouvelles listes.", http.StatusForbidden)
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("slug")))
	if titre == "" || !slugValide.MatchString(slug) {
		http.Error(w, "Il faut un titre, et une adresse en minuscules (lettres, chiffres, tirets).", http.StatusBadRequest)
		return
	}
	var codeUtilise string
	if app.politique == "invitation" {
		code := strings.TrimSpace(r.FormValue("code"))
		var n int
		app.db.QueryRow(`SELECT count(*) FROM codes_invitation WHERE code = ? AND utilise_le IS NULL`, code).Scan(&n)
		if n == 0 {
			http.Error(w, "Cette instance fonctionne sur code d'invitation.", http.StatusForbidden)
			return
		}
		codeUtilise = code
	}
	je := jeton(16)
	email := strings.TrimSpace(r.FormValue("email"))
	res, err := app.db.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat, email)
		VALUES (?,?,?,?,?,?)`,
		slug, je, titre, r.FormValue("lettre"), strings.TrimSpace(r.FormValue("etat")), nullSi(email))
	if err != nil {
		http.Error(w, "Cette adresse est déjà prise.", http.StatusConflict)
		return
	}
	listeID, _ := res.LastInsertId()
	if codeUtilise != "" {
		app.db.Exec(`UPDATE codes_invitation SET utilise_le = datetime('now'), liste_id = ? WHERE code = ?`,
			listeID, codeUtilise)
	}
	if email != "" {
		corps := fmt.Sprintf("Ta liste « %s » est créée.\n\nLien public, à partager : %s/l/%s\nLien secret d'édition, à garder : %s/e/%s\n",
			titre, app.baseURL, slug, app.baseURL, je)
		app.envoyer(email, "Perches — ton lien d'édition", corps, "recuperation_lien", 0, listeID)
	}
	app.rendre(w, "liste_creee.html", map[string]any{
		"TitrePage":   titre,
		"Titre":       titre,
		"LienPublic":  app.baseURL + "/l/" + slug,
		"LienEdition": app.baseURL + "/e/" + je,
		"EmailEnvoye": email != "",
	})
}

func (app *App) recupererLien(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email != "" {
		rows, err := app.db.Query(`SELECT `+colonnesListe+` FROM listes WHERE email = ?`, email)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				if l, err := scanListe(rows); err == nil {
					corps := fmt.Sprintf("Ta liste « %s » :\n\nLien public : %s/l/%s\nLien secret d'édition : %s/e/%s\n",
						l.Titre, app.baseURL, l.Slug, app.baseURL, l.JetonEdition)
					app.envoyer(email, "Perches — tes liens", corps, "recuperation_lien", 0, l.ID)
				}
			}
		}
	}
	app.rendre(w, "message.html", map[string]any{
		"TitrePage": "Perches",
		"Message":   "Si cette adresse est connue ici, un courriel vient de partir avec les liens.",
	})
}

// ---- pages publiques ----

func (app *App) voirListe(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	format := ""
	switch {
	case strings.HasSuffix(slug, ".ics"):
		format, slug = "ics", strings.TrimSuffix(slug, ".ics")
	case strings.HasSuffix(slug, ".json"):
		format, slug = "json", strings.TrimSuffix(slug, ".json")
	}
	liste, err := app.listeParSlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	intentions, err := app.intentionsPubliques(liste.ID)
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	switch format {
	case "ics":
		servirICS(w, liste.Titre, intentions, app.baseURL)
	case "json":
		servirJSONPublic(w, liste, intentions)
	default:
		app.rendre(w, "liste.html", map[string]any{
			"TitrePage":  liste.Titre,
			"OG":         map[string]string{"Titre": liste.Titre, "Description": premiereLigne(liste.Lettre), "URL": app.baseURL + "/l/" + liste.Slug},
			"Liste":      liste,
			"Intentions": intentions,
		})
	}
}

func premiereLigne(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len([]rune(s)) > 140 {
		s = string([]rune(s)[:140]) + "…"
	}
	return s
}

func (app *App) voirIntention(w http.ResponseWriter, r *http.Request) {
	j := r.PathValue("jeton")
	format := ""
	if strings.HasSuffix(j, ".ics") {
		format, j = "ics", strings.TrimSuffix(j, ".ics")
	}
	intention, liste, err := app.intentionParJeton(j)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if format == "ics" {
		servirICS(w, intention.Titre, []Intention{*intention}, app.baseURL)
		return
	}
	app.chargerReponses(intention)
	var jySerai, peutEtre []string
	if !intention.ReponsesEffacees() {
		for _, rep := range intention.Reponses {
			if !rep.PrenomVisible {
				continue
			}
			if rep.Statut == "jy_serai" {
				jySerai = append(jySerai, rep.Prenom)
			} else {
				peutEtre = append(peutEtre, rep.Prenom)
			}
		}
	}
	ogDesc := quandFR(intention.Quand)
	if intention.Lieu != "" {
		ogDesc += " — " + intention.Lieu
	}
	app.rendre(w, "intention.html", map[string]any{
		"TitrePage":        intention.Titre,
		"OG":               map[string]string{"Titre": intention.Titre, "Description": ogDesc, "URL": app.baseURL + "/i/" + intention.Jeton},
		"Liste":            liste,
		"I":                intention,
		"PrenomsJySerai":   jySerai,
		"PrenomsPeutEtre":  peutEtre,
		"FormulaireOuvert": !intention.AnnuleeLe.Valid && !intention.Passee(),
	})
}

func (app *App) repondre(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	intention, liste, err := app.intentionParJeton(r.PathValue("jeton"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if intention.AnnuleeLe.Valid || intention.Passee() {
		http.Error(w, "Cette intention est terminée.", http.StatusGone)
		return
	}
	prenom := strings.TrimSpace(r.FormValue("prenom"))
	statut := r.FormValue("statut")
	mot := strings.TrimSpace(r.FormValue("mot"))
	email := strings.TrimSpace(r.FormValue("email"))
	if prenom == "" || len([]rune(prenom)) > 60 {
		http.Error(w, "Un prénom suffit — mais il en faut un.", http.StatusBadRequest)
		return
	}
	if statut != "jy_serai" && statut != "peut_etre" {
		http.Error(w, "Les seules réponses possibles sont « j'y serai » et « peut-être ».", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(mot, "\r\n") || len([]rune(mot)) > 200 {
		http.Error(w, "Le mot tient sur une ligne (200 caractères au plus).", http.StatusBadRequest)
		return
	}
	visible := r.FormValue("prenom_visible") != ""
	if _, err := app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut, mot, prenom_visible, email)
		VALUES (?,?,?,?,?,?)`, intention.ID, prenom, statut, mot, visible, nullSi(email)); err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	// Convention 7 : aucune notification vers l'hôte — rien ne part d'ici.
	app.rendre(w, "reponse_ok.html", map[string]any{
		"TitrePage": intention.Titre,
		"Liste":     liste,
		"I":         intention,
		"Prenom":    prenom,
		"Statut":    statut,
		"AvecEmail": email != "",
	})
}

// ---- édition (identité = le jeton, rien d'autre) ----

func (app *App) editerListe(w http.ResponseWriter, r *http.Request) {
	j := r.PathValue("jeton")
	liste, err := app.listeParJetonEdition(j)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	intentions, err := app.toutesIntentions(liste.ID)
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	var aVenir, passees []Intention
	total := 0
	for k := range intentions {
		app.chargerReponses(&intentions[k])
		total += len(intentions[k].Reponses)
		if intentions[k].AnnuleeLe.Valid || intentions[k].Passee() {
			passees = append(passees, intentions[k])
		} else {
			aVenir = append(aVenir, intentions[k])
		}
	}
	app.rendre(w, "edition.html", map[string]any{
		"TitrePage":     liste.Titre + " — édition",
		"Liste":         liste,
		"AVenir":        aVenir,
		"Passees":       passees,
		"TotalReponses": total,
		"BaseURL":       app.baseURL,
	})
}

func (app *App) majListe(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	app.db.Exec(`UPDATE listes SET lettre = ?, etat = ?, email = ? WHERE id = ?`,
		r.FormValue("lettre"), strings.TrimSpace(r.FormValue("etat")),
		nullSi(strings.TrimSpace(r.FormValue("email"))), liste.ID)
	http.Redirect(w, r, "/e/"+liste.JetonEdition, http.StatusSeeOther)
}

func quandDepuisForm(r *http.Request) (string, error) {
	date := strings.TrimSpace(r.FormValue("date"))
	heure := strings.TrimSpace(r.FormValue("heure"))
	if date == "" {
		return "", fmt.Errorf("une intention v0 porte une date")
	}
	quand := date
	if heure != "" {
		quand += "T" + heure
	}
	if _, _, err := analyserQuand(quand); err != nil {
		return "", err
	}
	return quand, nil
}

func (app *App) creerIntention(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	if titre == "" {
		http.Error(w, "Il faut un titre.", http.StatusBadRequest)
		return
	}
	quand, err := quandDepuisForm(r)
	if err != nil {
		http.Error(w, "Il faut une date (la variante « à fixer » viendra plus tard).", http.StatusBadRequest)
		return
	}
	visibilite := "page"
	if r.FormValue("visibilite") == "lien" {
		visibilite = "lien"
	}
	var capacite any
	if c, err := strconv.Atoi(r.FormValue("capacite")); err == nil && c > 0 {
		capacite = c
	}
	jyVais := r.FormValue("jy_vais") != "0" // absent = oui : « j'y vais de toute façon » par défaut
	_, err = app.db.Exec(`INSERT INTO intentions
		(liste_id, jeton, titre, description, quand, lieu, url_externe, capacite, jy_vais_de_toute_facon, visibilite)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		liste.ID, jeton(12), titre, r.FormValue("description"), quand,
		strings.TrimSpace(r.FormValue("lieu")), nullSi(strings.TrimSpace(r.FormValue("url_externe"))),
		capacite, jyVais, visibilite)
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition, http.StatusSeeOther)
}

func (app *App) intentionDuChemin(w http.ResponseWriter, r *http.Request) (*Liste, *Intention, bool) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	intention, err := app.intentionDeListe(liste.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	return liste, intention, true
}

// notifierLogistique : convention 11 — seul ce qui ferait manquer le rendez-vous part.
func (app *App) notifierLogistique(intentionID int64, sujet, message string) {
	rows, err := app.db.Query(`SELECT id, email FROM reponses WHERE intention_id = ? AND email IS NOT NULL`, intentionID)
	if err != nil {
		return
	}
	defer rows.Close()
	type dest struct {
		id    int64
		email string
	}
	var dests []dest
	for rows.Next() {
		var d dest
		if rows.Scan(&d.id, &d.email) == nil {
			dests = append(dests, d)
		}
	}
	corps := message + "\n\nRien ne t'est demandé : le silence vaut « pas cette fois »."
	for _, d := range dests {
		app.envoyer(d.email, "Perches — "+sujet, corps, "logistique", d.id, 0)
	}
}

func (app *App) annulerIntention(w http.ResponseWriter, r *http.Request) {
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	if !intention.AnnuleeLe.Valid {
		app.db.Exec(`UPDATE intentions SET annulee_le = datetime('now') WHERE id = ?`, intention.ID)
		app.notifierLogistique(intention.ID, "annulation : "+intention.Titre,
			fmt.Sprintf("L'intention « %s » (%s) est annulée.", intention.Titre, quandFR(intention.Quand)))
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition, http.StatusSeeOther)
}

func (app *App) majIntention(w http.ResponseWriter, r *http.Request) {
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	quand, err := quandDepuisForm(r)
	if err != nil {
		http.Error(w, "Il faut une date valide.", http.StatusBadRequest)
		return
	}
	lieu := strings.TrimSpace(r.FormValue("lieu"))
	change := quand != intention.Quand.String || lieu != intention.Lieu
	app.db.Exec(`UPDATE intentions SET quand = ?, lieu = ? WHERE id = ?`, quand, lieu, intention.ID)
	if change && !intention.AnnuleeLe.Valid {
		var q = quand
		msg := fmt.Sprintf("L'intention « %s » a changé : %s", intention.Titre,
			quandFR(sqlString(q)))
		if lieu != "" {
			msg += " — " + lieu
		}
		msg += fmt.Sprintf(".\n\n%s/i/%s", app.baseURL, intention.Jeton)
		app.notifierLogistique(intention.ID, "changement : "+intention.Titre, msg)
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition, http.StatusSeeOther)
}

func (app *App) effacerReponse(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	app.db.Exec(`DELETE FROM reponses WHERE id = ?
		AND intention_id IN (SELECT id FROM intentions WHERE liste_id = ?)`, id, liste.ID)
	http.Redirect(w, r, "/e/"+liste.JetonEdition, http.StatusSeeOther)
}
