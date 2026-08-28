package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var slugValide = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)

func (app *App) rendre(w http.ResponseWriter, r *http.Request, nom string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if m, ok := data.(map[string]any); ok {
		m["Theme"] = themeDe(r)
	}
	if err := app.tpl.ExecuteTemplate(w, nom, data); err != nil {
		log.Printf("template %s : %v", nom, err)
	}
}

func (app *App) tropVite(w http.ResponseWriter, r *http.Request) bool {
	if app.limiteur.Autorise(app.ipDe(r)) {
		return false
	}
	app.erreur(w, r, http.StatusTooManyRequests, "Trop de demandes — réessaie dans une minute.")
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
	// Le navigateur connaît l'édition : on y va, sans détour par la présentation.
	if c, err := r.Cookie("atelier"); err == nil {
		if l, err := app.listeParJetonEdition(c.Value); err == nil {
			http.Redirect(w, r, "/e/"+l.JetonEdition, http.StatusSeeOther)
			return
		}
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	codeInvalide := false
	if code != "" && app.politique == "invitation" {
		var n int
		app.db.QueryRow(`SELECT count(*) FROM codes_invitation WHERE code = ? AND utilise_le IS NULL AND cree_le > datetime('now', '-30 days')`, code).Scan(&n)
		if n == 0 {
			code, codeInvalide = "", true
		}
	}
	app.rendre(w, r, "accueil.html", map[string]any{
		"TitrePage":    "Perches",
		"Politique":    app.politique,
		"BaseURL":      app.baseURL,
		"Code":         code,
		"CodeInvalide": codeInvalide,
	})
}

// Le navigateur de l'hôte retient son édition (le cookie garde son nom historique) : taper l'adresse du site suffit
// ensuite à y revenir. Cookie côté hôte uniquement — l'invité n'en reçoit jamais.
func (app *App) retenirAtelier(w http.ResponseWriter, jeton string) {
	http.SetCookie(w, &http.Cookie{
		Name: "atelier", Value: jeton, Path: "/", MaxAge: 365 * 24 * 3600,
		HttpOnly: true, Secure: strings.HasPrefix(app.baseURL, "https"), SameSite: http.SameSiteLaxMode,
	})
}

func (app *App) oublierAtelier(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		app.erreur(w, r, http.StatusForbidden, "Ce geste se fait depuis Perches.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "atelier", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (app *App) creerListe(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	if app.politique == "fermee" {
		app.erreur(w, r, http.StatusForbidden, "Cette instance n'accepte pas de nouvelles listes.")
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	if titre == "" || len([]rune(titre)) > 80 {
		app.erreur(w, r, http.StatusBadRequest, "Il faut un titre (80 caractères au plus).")
		return
	}
	email := emailPlausible(r.FormValue("email"))
	if email == "" {
		app.erreur(w, r, http.StatusBadRequest, "Il faut un e-mail : c'est lui qui renvoie le lien d'édition.")
		return
	}
	if app.politique == "ouverte" {
		// Sur un autre appareil, « ouvrir ta liste » est souvent une tentative de la retrouver :
		// si l'e-mail a déjà une liste, on renvoie l'édition au lieu d'en créer une deuxième.
		var l Liste
		if err := app.db.QueryRow(`SELECT `+colonnesListe+` FROM listes WHERE email = ? ORDER BY cree_le LIMIT 1`, email).
			Scan(&l.ID, &l.Slug, &l.JetonEdition, &l.Titre, &l.Lettre, &l.Etat, &l.Email, &l.FermeeLe, &l.CreeLe); err == nil {
			app.enFond(func() {
				corps := fmt.Sprintf("Ta liste « %s » :\n\nTa page, à partager : %s/l/%s\nTon édition (lien secret, garde-le pour toi) : %s/e/%s\n",
					l.Titre, app.baseURL, l.Slug, app.baseURL, l.JetonEdition)
				app.envoyer(email, "Perches — ton lien d'édition", corps, "recuperation_lien", 0, l.ID)
			})
			app.erreur(w, r, http.StatusOK, "Cet e-mail a déjà sa liste, « "+l.Titre+" » — le lien d'édition vient de repartir par e-mail.")
			return
		}
	}
	// Une transaction : le code est consommé d'abord (une seule liste par code, même en
	// concurrence), puis l'adresse est essayée jusqu'à en trouver une libre.
	tx, err := app.db.Begin()
	if err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	defer tx.Rollback()
	code := strings.TrimSpace(r.FormValue("code"))
	if app.politique == "invitation" {
		res, err := tx.Exec(`UPDATE codes_invitation SET utilise_le = datetime('now')
			WHERE code = ? AND utilise_le IS NULL AND cree_le > datetime('now', '-30 days')`, code)
		if n, _ := res.RowsAffected(); err != nil || n != 1 {
			app.erreur(w, r, http.StatusForbidden, "Ce lien d'invitation a déjà servi, ou n'est plus valable — redemande-en un à la personne qui te l'a envoyé.")
			return
		}
	}
	je := jeton(16)
	base := slugDe(titre)
	slug := base
	var res sql.Result
	for n := 2; ; n++ {
		res, err = tx.Exec(`INSERT INTO listes (slug, jeton_edition, titre, lettre, etat, email) VALUES (?,?,?,'','',?)`,
			slug, je, titre, email)
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "UNIQUE") || n > 100 {
			app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
			return
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	listeID, _ := res.LastInsertId()
	if app.politique == "invitation" {
		tx.Exec(`UPDATE codes_invitation SET liste_id = ? WHERE code = ?`, listeID, code)
	}
	if err := tx.Commit(); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	if email != "" {
		corps := fmt.Sprintf("Ta liste « %s » est ouverte.\n\nTa page, à partager : %s/l/%s\nTon édition (lien secret, garde-le pour toi) : %s/e/%s\n",
			titre, app.baseURL, slug, app.baseURL, je)
		app.envoyer(email, "Perches — ton lien d'édition", corps, "recuperation_lien", 0, listeID)
	}
	http.Redirect(w, r, "/e/"+je+"?bienvenue=1", http.StatusSeeOther)
}

func (app *App) recupererLien(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email != "" {
		// On lit tout, on ferme, puis on envoie : avec une seule connexion SQLite,
		// écrire dans `envois` pendant que `rows` est ouvert bloquerait tout le service.
		var listes []Liste
		if rows, err := app.db.Query(`SELECT `+colonnesListe+` FROM listes WHERE email = ?`, email); err == nil {
			for rows.Next() {
				if l, err := scanListe(rows); err == nil {
					listes = append(listes, *l)
				}
			}
			rows.Close()
		}
		app.enFond(func() {
			for _, l := range listes {
				corps := fmt.Sprintf("Ta liste « %s » :\n\nTa page, à partager : %s/l/%s\nTon édition (lien secret, garde-le pour toi) : %s/e/%s\n",
					l.Titre, app.baseURL, l.Slug, app.baseURL, l.JetonEdition)
				app.envoyer(email, "Perches — ton lien d'édition", corps, "recuperation_lien", 0, l.ID)
			}
		})
	}
	app.rendre(w, r, "message.html", map[string]any{
		"TitrePage": "Perches",
		"Message":   "Si cet e-mail est connu ici, le lien d'édition vient de partir.",
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
	liste, err := app.listeParSlug(strings.ToLower(slug))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	if liste.FermeeLe.Valid {
		liste.Lettre = ""
	}
	toutes, err := app.intentionsPubliques(liste.ID)
	if err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	// même règle que la page de la perche : passée deux heures après l'heure, ou à minuit sans heure.
	// Filtre « Perches » (?perches) : la chronologie ne garde que les événements où une perche est tendue.
	filtre := r.URL.Query().Has("perches")
	var intentions []Intention
	for _, i := range toutes {
		if !i.Passee() && !liste.FermeeLe.Valid && (!filtre || i.Tendue()) {
			intentions = append(intentions, i)
		}
	}
	switch format {
	case "ics":
		servirICS(w, liste.Titre, intentions, app.baseURL)
	case "json":
		servirJSONPublic(w, liste, intentions)
	default:
		merci, percheMerci := merciDe(r), r.URL.Query().Get("perche")
		vues, nbPerches := []VuePerche{}, 0
		for k := range intentions {
			var m map[string]any
			if merci != nil && intentions[k].Jeton == percheMerci {
				m = merci
			}
			if intentions[k].Tendue() {
				nbPerches++
			}
			vues = append(vues, app.vuePerche(&intentions[k], liste, m, true))
		}
		app.rendre(w, r, "liste.html", map[string]any{
			"TitrePage": liste.Titre,
			"OG":        app.og(liste.Titre, descriptionOG(sansMarkdown(liste.Lettre), nbPerches), "/l/"+liste.Slug),
			"Alternate": app.baseURL + "/l/" + liste.Slug + ".json",
			"Liste":     liste,
			"Perches":   vues,
			"NbPerches": nbPerches,
			"Filtre":    filtre,
		})
	}
}

// descriptionOG : ce que montre la carte de partage. Une salutation seule sur sa ligne
// (« Bonjour, ») ne dit rien : on prend la première ligne de plus de 40 caractères.
func descriptionOG(lettre string, nbPerches int) string {
	for _, l := range strings.Split(lettre, "\n") {
		l = strings.TrimSpace(l)
		if len([]rune(l)) > 40 {
			return couper(l, 140)
		}
	}
	switch nbPerches {
	case 0:
		return "Une liste d'intentions, sans obligation."
	case 1:
		return "Une perche — une liste d'intentions, sans obligation."
	}
	return fmt.Sprintf("%d perches — une liste d'intentions, sans obligation.", nbPerches)
}

func couper(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n]), " ,;:") + "…"
}

func (app *App) og(titre, description, chemin string) map[string]string {
	return map[string]string{"Titre": titre, "Description": description,
		"URL": app.baseURL + chemin, "Image": app.baseURL + "/static/carte.png"}
}

// phraseAuraitAime : « Anna aurait bien aimé. », « Anna et Marc auraient bien aimé. »
func phraseAuraitAime(prenoms []string) string {
	switch len(prenoms) {
	case 0:
		return ""
	case 1:
		return prenoms[0] + " aurait bien aimé."
	}
	return strings.Join(prenoms[:len(prenoms)-1], ", ") + " et " + prenoms[len(prenoms)-1] + " auraient bien aimé."
}

// phrasePresences : « Seront là : Léa, Tom, et une personne discrète. »
func phrasePresences(visibles []string, discrets int) string {
	if len(visibles)+discrets == 0 {
		return ""
	}
	parts := append([]string{}, visibles...)
	switch discrets {
	case 0:
	case 1:
		parts = append(parts, "une personne discrète")
	default:
		parts = append(parts, fmt.Sprintf("%d personnes discrètes", discrets))
	}
	return "Seront là : " + strings.Join(parts, ", ") + "."
}

func (app *App) voirIntention(w http.ResponseWriter, r *http.Request) {
	j := r.PathValue("jeton")
	format := ""
	if strings.HasSuffix(j, ".ics") {
		format, j = "ics", strings.TrimSuffix(j, ".ics")
	}
	intention, liste, err := app.intentionParJeton(j)
	if err != nil {
		app.introuvable(w, r)
		return
	}
	if format == "ics" {
		servirICS(w, intention.Titre, []Intention{*intention}, app.baseURL)
		return
	}
	v := app.vuePerche(intention, liste, merciDe(r), false)
	if liste.FermeeLe.Valid {
		liste.Lettre = ""
	}
	ogDesc := intention.QuandFR()
	if intention.Lieu != "" {
		ogDesc += " — " + intention.Lieu
	}
	app.rendre(w, r, "intention.html", map[string]any{
		"TitrePage":     intention.Titre,
		"OG":            app.og(intention.Titre, ogDesc, "/i/"+intention.Jeton),
		"Liste":         liste,
		"V":             v,
		"LienSeulement": intention.Visibilite == "lien",
		"Voix":          couper(strings.TrimSpace(sansMarkdown(liste.Lettre)), 280),
	})
}

func (app *App) repondre(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	intention, liste, err := app.intentionParJeton(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	if intention.Repere() {
		app.erreur(w, r, http.StatusGone, "Un repéré ne prend pas de réponse : il est signalé, sans engagement.")
		return
	}
	if intention.AnnuleeLe.Valid || intention.PerchePassee() || liste.FermeeLe.Valid {
		app.erreur(w, r, http.StatusGone, "Cette perche ne prend plus de réponse.")
		return
	}
	prenom := strings.TrimSpace(r.FormValue("prenom"))
	statut := r.FormValue("statut")
	mot := strings.TrimSpace(r.FormValue("mot"))
	email := emailPlausible(r.FormValue("email"))
	if prenom == "" || len([]rune(prenom)) > 60 {
		app.erreur(w, r, http.StatusBadRequest, "Il faut un prénom.")
		return
	}
	if statut != "jy_serai" && statut != "peut_etre" && statut != "jaurais_aime" {
		app.erreur(w, r, http.StatusBadRequest, "Les réponses possibles : « j'y serai », « peut-être », « j'aurais bien aimé ».")
		return
	}
	// Pot de miel : un champ invisible qu'un humain ne remplit pas. On répond comme si de rien.
	if r.FormValue("verif") != "" {
		http.Redirect(w, r, "/i/"+intention.Jeton, http.StatusSeeOther)
		return
	}
	if strings.ContainsAny(mot, "\r\n") || len([]rune(mot)) > 200 {
		app.erreur(w, r, http.StatusBadRequest, "Le mot tient sur une ligne (200 caractères au plus).")
		return
	}
	visible := r.FormValue("prenom_visible") != ""
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE intention_id = ?`, intention.ID).Scan(&n)
	if n >= plafondReponses {
		app.erreur(w, r, http.StatusForbidden, "Cette perche a atteint son nombre maximal de réponses — écris directement à la personne.")
		return
	}
	if email != "" {
		// Un e-mail ne s'inscrit qu'une fois par perche : un rappel par personne, pas par clic.
		app.db.QueryRow(`SELECT count(*) FROM reponses WHERE intention_id = ? AND email = ? AND lower(prenom) <> lower(?)`, intention.ID, email, prenom).Scan(&n)
		if n > 0 {
			email = ""
		}
	}
	// Une réponse par prénom et par perche : revenir la veille et redonner son prénom remplace,
	// sans cookie ni compte (convention 8).
	var existante int64
	app.db.QueryRow(`SELECT id FROM reponses WHERE intention_id = ? AND lower(prenom) = lower(?)`, intention.ID, prenom).Scan(&existante)
	err = nil
	if existante != 0 {
		if email == "" {
			_, err = app.db.Exec(`UPDATE reponses SET prenom = ?, statut = ?, mot = ?, prenom_visible = ?, cree_le = datetime('now') WHERE id = ?`,
				prenom, statut, mot, visible, existante)
		} else {
			_, err = app.db.Exec(`UPDATE reponses SET prenom = ?, statut = ?, mot = ?, prenom_visible = ?, email = ?, cree_le = datetime('now') WHERE id = ?`,
				prenom, statut, mot, visible, email, existante)
		}
	} else {
		_, err = app.db.Exec(`INSERT INTO reponses (intention_id, prenom, statut, mot, prenom_visible, email)
			VALUES (?,?,?,?,?,?)`, intention.ID, prenom, statut, mot, visible, nullSi(email))
	}
	if err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	// Convention 7 : aucune notification vers l'hôte — rien ne part d'ici.
	// Redirection : recharger la page ne redépose pas la réponse.
	q := url.Values{"merci": {statut}, "prenom": {prenom}}
	if email != "" {
		q.Set("email", "1")
	}
	if r.FormValue("retour") == "liste" {
		q.Set("perche", intention.Jeton)
		http.Redirect(w, r, "/l/"+liste.Slug+"?"+q.Encode()+"#p-"+intention.Jeton, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/i/"+intention.Jeton+"?"+q.Encode(), http.StatusSeeOther)
}

// ---- édition (identité = le jeton, rien d'autre) ----

func (app *App) editerListe(w http.ResponseWriter, r *http.Request) {
	j := r.PathValue("jeton")
	liste, err := app.listeParJetonEdition(j)
	if err != nil {
		app.introuvable(w, r)
		return
	}
	intentions, err := app.toutesIntentions(liste.ID)
	if err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	app.retenirAtelier(w, liste.JetonEdition)
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
	app.rendre(w, r, "edition.html", map[string]any{
		"TitrePage":     liste.Titre + " — édition",
		"Liste":         liste,
		"AVenir":        aVenir,
		"Passees":       passees,
		"TotalReponses": total,
		"BaseURL":       app.baseURL,
		"LienPublic":    app.baseURL + "/l/" + liste.Slug,
		"LienEdition":   app.baseURL + "/e/" + liste.JetonEdition,
		"Bienvenue":     r.URL.Query().Has("bienvenue"),
		"Invitation":    r.URL.Query().Get("invitation"),
		"Politique":     app.politique,
		"OK":            r.URL.Query().Get("ok"),
	})
}

// creerInvitation : un hôte ouvre la porte à un ami — un lien à usage unique vers le
// formulaire d'ouverture. Redirection après POST : recharger ne crée pas de doublon.
func (app *App) creerInvitation(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	code := jeton(8)
	if _, err := app.db.Exec(`INSERT INTO codes_invitation (code) VALUES (?)`, code); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition+"/inviter?invitation="+code, http.StatusSeeOther)
}

func (app *App) majListe(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	lettre := r.FormValue("lettre")
	if !borne(lettre, 5000) {
		app.erreur(w, r, http.StatusBadRequest, "L'introduction tient en 5 000 caractères.")
		return
	}
	if _, err := app.db.Exec(`UPDATE listes SET lettre = ?, etat = '' WHERE id = ?`, lettre, liste.ID); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition+"?ok=lettre#lettre", http.StatusSeeOther)
}

// reglages : titre, adresse, e-mail — ce qui change rarement, sur sa propre page derrière le menu.
func (app *App) reglages(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("slug")))
	email := emailPlausible(r.FormValue("email"))
	if titre == "" || len([]rune(titre)) > 80 || !slugValide.MatchString(slug) || email == "" {
		app.erreur(w, r, http.StatusBadRequest, "Il faut un titre, une adresse en minuscules (lettres, chiffres, tirets) et un e-mail.")
		return
	}
	if _, err := app.db.Exec(`UPDATE listes SET titre = ?, slug = ?, email = ? WHERE id = ?`, titre, slug, email, liste.ID); err != nil {
		app.erreur(w, r, http.StatusConflict, "Cette adresse est déjà prise par une autre liste.")
		return
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition+"/reglages?ok", http.StatusSeeOther)
}

// datesDepuisForm : la date (avec l'heure si donnée) et, si ça dure, le dernier jour — qui doit
// suivre le premier. Une fin égale au premier jour n'en est pas une. Le préfixe distingue les
// dates de l'événement (« date », « heure », « fin ») de celles de la perche (« perche_date »…).
func datesDepuisForm(r *http.Request, prefixe string) (quand string, fin any, err error) {
	date := strings.TrimSpace(r.FormValue(prefixe + "date"))
	heure := strings.TrimSpace(r.FormValue(prefixe + "heure"))
	if date == "" {
		return "", nil, fmt.Errorf("il faut une date")
	}
	quand = date
	if heure != "" {
		quand += "T" + heure
	}
	if _, _, err := analyserQuand(quand); err != nil {
		return "", nil, err
	}
	if f := strings.TrimSpace(r.FormValue(prefixe + "fin")); f != "" && f != date {
		if _, _, err := analyserQuand(f); err != nil || f < date {
			return "", nil, fmt.Errorf("la fin précède le début")
		}
		fin = f
	}
	return quand, fin, nil
}

func quandDepuisForm(r *http.Request) (string, any, error) { return datesDepuisForm(r, "") }

// datesPercheDepuisForm : quand l'hôte y va — ses dates si le formulaire en donne, sinon
// celles de l'événement.
func datesPercheDepuisForm(r *http.Request, quand string, fin any) (string, any, error) {
	if strings.TrimSpace(r.FormValue("perche_date")) == "" {
		return quand, fin, nil
	}
	return datesDepuisForm(r, "perche_")
}

func (app *App) creerIntention(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	if titre == "" {
		app.erreur(w, r, http.StatusBadRequest, "Il faut un titre : ce que tu vas faire, en quelques mots.")
		return
	}
	if msg := champsIntentionTropLongs(r); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	quand, fin, err := quandDepuisForm(r)
	if err != nil {
		app.erreur(w, r, http.StatusBadRequest, "Il faut une date, et si la perche dure plusieurs jours, une fin qui suit le début.")
		return
	}
	visibilite := "page"
	if r.FormValue("visibilite") == "lien" {
		visibilite = "lien"
	}
	// Lot F : tout est repéré ; la perche est un geste posé dessus, avec ses propres dates.
	var tendueLe, percheQuand, percheFin any
	if r.FormValue("tendre") != "" {
		pq, pf, err := datesPercheDepuisForm(r, quand, fin)
		if err != nil {
			app.erreur(w, r, http.StatusBadRequest, "Quand j'y vais : il faut une date valide, et une fin qui suit le début.")
			return
		}
		tendueLe, percheQuand, percheFin = "now", pq, pf
	}
	jyVais := true // décision 2026-08-28 : « j'y vais de toute façon », sans option
	res, err := app.db.Exec(`INSERT INTO intentions
		(liste_id, jeton, titre, description, quand, fin, lieu, url_externe, jy_vais_de_toute_facon,
		 perche_tendue_le, perche_quand, perche_fin, visibilite)
		VALUES (?,?,?,?,?,?,?,?,?, CASE WHEN ? IS NULL THEN NULL ELSE datetime('now') END, ?, ?, ?)`,
		liste.ID, jeton(12), titre, r.FormValue("description"), quand, fin,
		strings.TrimSpace(r.FormValue("lieu")), nullSi(urlPlausible(r.FormValue("url_externe"))),
		jyVais, tendueLe, percheQuand, percheFin, visibilite)
	if err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	id, _ := res.LastInsertId()
	http.Redirect(w, r, fmt.Sprintf("/e/%s?ok=ajoute#perche-%d", liste.JetonEdition, id), http.StatusSeeOther)
}

func (app *App) intentionDuChemin(w http.ResponseWriter, r *http.Request) (*Liste, *Intention, bool) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return nil, nil, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		app.introuvable(w, r)
		return nil, nil, false
	}
	intention, err := app.intentionDeListe(liste.ID, id)
	if err != nil {
		app.introuvable(w, r)
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
	corps := message
	app.enFond(func() {
		for _, d := range dests {
			app.envoyer(d.email, "Perches — "+sujet, corps, "logistique", d.id, 0)
		}
	})
}

const plafondReponses = 200

// champsIntentionTropLongs : bornes serveur des champs libres d'une perche.
func champsIntentionTropLongs(r *http.Request) string {
	switch {
	case !borne(r.FormValue("titre"), 120):
		return "Le titre tient en 120 caractères."
	case !borne(r.FormValue("lieu"), 120):
		return "Le lieu tient en 120 caractères."
	case !borne(r.FormValue("description"), 5000):
		return "Les quelques mots tiennent en 5 000 caractères."
	}
	return ""
}

func (app *App) annulerIntention(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	if !intention.AnnuleeLe.Valid {
		if _, err := app.db.Exec(`UPDATE intentions SET annulee_le = datetime('now') WHERE id = ?`, intention.ID); err != nil {
			app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
			return
		}
		if intention.Tendue() {
			app.notifierLogistique(intention.ID, "annulation : "+intention.Titre,
				fmt.Sprintf("« %s » (%s) est annulé.\n\n%s/i/%s", intention.Titre, intention.PercheQuandFR(), app.baseURL, intention.Jeton))
		}
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition+"?ok=annulee#archive", http.StatusSeeOther)
}

func (app *App) majIntention(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	if msg := champsIntentionTropLongs(r); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	quand, fin, err := quandDepuisForm(r)
	if err != nil {
		app.erreur(w, r, http.StatusBadRequest, "Il faut une date valide, et si la perche dure plusieurs jours, une fin qui suit le début.")
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	if titre == "" {
		app.erreur(w, r, http.StatusBadRequest, "Il faut un titre : ce que tu vas faire, en quelques mots.")
		return
	}
	lieu := strings.TrimSpace(r.FormValue("lieu"))
	visibilite := "page"
	if r.FormValue("visibilite") == "lien" {
		visibilite = "lien"
	}
	jyVais := true
	// Tout se corrige ; seuls les dates et le lieu — la logistique — valent un mot aux invités.
	finTexte, _ := fin.(string)
	change := quand != intention.Quand.String || lieu != intention.Lieu || finTexte != intention.Fin.String
	if _, err := app.db.Exec(`UPDATE intentions SET titre = ?, description = ?, quand = ?, fin = ?, lieu = ?, url_externe = ?,
		visibilite = ?, jy_vais_de_toute_facon = ? WHERE id = ?`,
		titre, r.FormValue("description"), quand, fin, lieu, nullSi(urlPlausible(r.FormValue("url_externe"))),
		visibilite, jyVais, intention.ID); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	// Une perche tendue « tout du long » suit les dates de l'événement quand elles bougent.
	if intention.Tendue() && intention.PercheAuxDatesDeLEvenement() {
		app.db.Exec(`UPDATE intentions SET perche_quand = ?, perche_fin = ? WHERE id = ?`, quand, fin, intention.ID)
		intention.PercheQuand, intention.PercheFin = sqlString(quand), sqlString(finTexte)
	}
	intention.Titre, intention.Quand, intention.Fin = titre, sqlString(quand), sqlString(finTexte)
	if change && intention.Tendue() && !intention.AnnuleeLe.Valid {
		msg := fmt.Sprintf("« %s » change : %s", intention.Titre, intention.PercheQuandFR())
		if lieu != "" {
			msg += " — " + lieu
		}
		msg += fmt.Sprintf(".\n\n%s/i/%s", app.baseURL, intention.Jeton)
		app.notifierLogistique(intention.ID, "« "+intention.Titre+" » change de date ou de lieu", msg)
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?ok=corrige#perche-%d", liste.JetonEdition, intention.ID), http.StatusSeeOther)
}

func (app *App) effacerReponse(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		app.introuvable(w, r)
		return
	}
	if _, err := app.db.Exec(`DELETE FROM reponses WHERE id = ?
		AND intention_id IN (SELECT id FROM intentions WHERE liste_id = ?)`, id, liste.ID); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition+"#perches", http.StatusSeeOther)
}
