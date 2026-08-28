package main

import (
	"fmt"
	"net/http"
	"strings"
)

// Les gestes rares et irréversibles de l'édition passent par une page de confirmation
// (décision 2026-08-28) ; les erreurs et les liens cassés parlent la langue du produit.

// erreur : une page dans le gabarit, avec un chemin de retour déduit de l'URL.
func (app *App) erreur(w http.ResponseWriter, r *http.Request, code int, message string) {
	retour, libelle := "/", "revenir à l'accueil"
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(p) >= 2 && p[0] == "e":
		retour, libelle = "/e/"+p[1], "revenir à l'édition"
	case len(p) >= 2 && p[0] == "i":
		retour, libelle = "/i/"+p[1], "revenir à la perche"
	case len(p) >= 2 && p[0] == "l":
		retour, libelle = "/l/"+p[1], "revenir à la page"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	app.tpl.ExecuteTemplate(w, "message.html", map[string]any{
		"TitrePage": "Perches", "Message": message, "Retour": retour, "Libelle": libelle,
	})
}

// introuvable : un lien abîmé par une messagerie (point final, slash, majuscule) est réparé
// quand c'est possible ; sinon on le dit en français, avec la clé de secours pour une édition.
func (app *App) introuvable(w http.ResponseWriter, r *http.Request) {
	chemin := r.URL.Path
	repare := strings.TrimRight(chemin, "./,)»")
	if strings.HasPrefix(repare, "/l/") {
		repare = strings.ToLower(repare)
	}
	if repare != chemin && repare != "" && repare != "/" {
		http.Redirect(w, r, repare, http.StatusMovedPermanently)
		return
	}
	message := "Ce lien ne mène nulle part — vérifie qu'il est complet."
	if strings.HasPrefix(chemin, "/e/") {
		message = "Ce lien n'ouvre aucune édition — est-il complet ? Depuis l'accueil, ton e-mail peut le renvoyer."
	}
	app.erreur(w, r, http.StatusNotFound, message)
}

func (app *App) confirmer(w http.ResponseWriter, titre, question, action, bouton, retour string) {
	app.rendre(w, "confirmer.html", map[string]any{
		"TitrePage": titre, "Titre": titre, "Question": question,
		"Action": action, "Bouton": bouton, "Retour": retour,
	})
}

func (app *App) nbAvecEmail(intentionID int64) int {
	var n int
	app.db.QueryRow(`SELECT count(*) FROM reponses WHERE intention_id = ? AND email IS NOT NULL`, intentionID).Scan(&n)
	return n
}

func prevenus(n int) string {
	switch n {
	case 0:
		return "Personne n'a laissé d'e-mail : rien ne partira."
	case 1:
		return "La personne qui a laissé un e-mail sera prévenue."
	}
	return fmt.Sprintf("Les %d personnes qui ont laissé un e-mail seront prévenues.", n)
}

func (app *App) confirmerAnnulation(w http.ResponseWriter, r *http.Request) {
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	app.confirmer(w, "Annuler « "+intention.Titre+" » ?",
		prevenus(app.nbAvecEmail(intention.ID))+" Tu pourras rétablir la perche tant que la date n'est pas passée.",
		fmt.Sprintf("/e/%s/intentions/%d/annuler", liste.JetonEdition, intention.ID), "Oui, annuler", "/e/"+liste.JetonEdition)
}

func (app *App) retablirIntention(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	if intention.AnnuleeLe.Valid && !intention.Passee() {
		if _, err := app.db.Exec(`UPDATE intentions SET annulee_le = NULL WHERE id = ?`, intention.ID); err != nil {
			app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
			return
		}
		app.notifierLogistique(intention.ID, "« "+intention.Titre+" » est de nouveau d'actualité",
			fmt.Sprintf("« %s » (%s) est finalement maintenu.\n\n%s/i/%s", intention.Titre, intention.QuandFR(), app.baseURL, intention.Jeton))
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?ok=retablie#perche-%d", liste.JetonEdition, intention.ID), http.StatusSeeOther)
}

func (app *App) confirmerEffacement(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	var prenom, mot string
	err = app.db.QueryRow(`SELECT r.prenom, r.mot FROM reponses r JOIN intentions i ON i.id = r.intention_id
		WHERE r.id = ? AND i.liste_id = ?`, r.PathValue("id"), liste.ID).Scan(&prenom, &mot)
	if err != nil {
		app.introuvable(w, r)
		return
	}
	question := "Sa réponse disparaîtra de la page et de l'édition, sans retour. Rien ne lui est envoyé."
	if mot != "" {
		question = "Son mot — « " + mot + " » — disparaîtra avec. " + question
	}
	app.confirmer(w, "Effacer la réponse de "+prenom+" ?", question,
		"/e/"+liste.JetonEdition+"/reponses/"+r.PathValue("id")+"/effacer", "Oui, effacer", "/e/"+liste.JetonEdition)
}

func (app *App) confirmerFermeture(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	app.confirmer(w, "Fermer « "+liste.Titre+" » ?",
		"Ta page dira « fermée pour le moment », sans perches ni introduction. Personne n'est prévenu. Tout reste ici, et tu peux rouvrir quand tu veux.",
		"/e/"+liste.JetonEdition+"/fermer", "Oui, fermer", "/e/"+liste.JetonEdition)
}

func (app *App) fermerListe(w http.ResponseWriter, r *http.Request) {
	app.basculerFermeture(w, r, true)
}

func (app *App) rouvrirListe(w http.ResponseWriter, r *http.Request) {
	app.basculerFermeture(w, r, false)
}

func (app *App) basculerFermeture(w http.ResponseWriter, r *http.Request, fermer bool) {
	if app.tropVite(w, r) {
		return
	}
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	var valeur any
	if fermer {
		valeur = "now"
	}
	if _, err := app.db.Exec(`UPDATE listes SET fermee_le = CASE WHEN ? IS NULL THEN NULL ELSE datetime('now') END WHERE id = ?`, valeur, liste.ID); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	http.Redirect(w, r, "/e/"+liste.JetonEdition, http.StatusSeeOther)
}
