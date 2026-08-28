package main

import (
	"fmt"
	"net/http"
	"strings"
)

// Seconde passe, lot B2 et menu (décisions 2026-08-28) : la perche se lit et se répond sur
// place, dans la page de la liste ; l'édition range ses gestes rares dans un menu ; le thème
// se choisit côté serveur, par cookie — l'invité n'en reçoit toujours aucun.

// VuePerche : ce que la page d'une perche et sa carte dans la liste ont en commun.
type VuePerche struct {
	I           *Intention
	Liste       *Liste
	Tendue      bool // une perche est posée sur ce repéré
	Ouverte     bool // la perche prend encore des réponses
	Presences   string
	PeutEtre    []string
	AuraitAime  string
	Merci       map[string]any
	DepuisListe bool // le formulaire ramène à la liste, carte ouverte
	Ouvrir      bool // la carte s'ouvre d'elle-même (après une réponse)
}

func (app *App) vuePerche(i *Intention, liste *Liste, merci map[string]any, depuisListe bool) VuePerche {
	app.chargerReponses(i)
	var jySerai, peutEtre, auraitAime []string
	discrets := 0
	if !i.ReponsesEffacees() {
		for _, rep := range i.Reponses {
			if !rep.PrenomVisible {
				if rep.Statut == "jy_serai" {
					discrets++
				}
				continue
			}
			switch rep.Statut {
			case "jy_serai":
				jySerai = append(jySerai, rep.Prenom)
			case "peut_etre":
				peutEtre = append(peutEtre, rep.Prenom)
			default:
				auraitAime = append(auraitAime, rep.Prenom)
			}
		}
	}
	return VuePerche{
		I: i, Liste: liste,
		Tendue:     i.Tendue(),
		Ouverte:    i.Tendue() && !i.AnnuleeLe.Valid && !i.PerchePassee() && !liste.FermeeLe.Valid,
		Presences:  phrasePresences(jySerai, discrets),
		PeutEtre:   peutEtre,
		AuraitAime: phraseAuraitAime(auraitAime),
		Merci:      merci, DepuisListe: depuisListe, Ouvrir: merci != nil,
	}
}

// merciDe : le mot de confirmation après une réponse, porté par l'URL de redirection.
func merciDe(r *http.Request) map[string]any {
	s := r.URL.Query().Get("merci")
	if s != "jy_serai" && s != "peut_etre" && s != "jaurais_aime" {
		return nil
	}
	return map[string]any{"Statut": s, "Prenom": r.URL.Query().Get("prenom"), "AvecEmail": r.URL.Query().Get("email") == "1"}
}

// ---- thème ----

// themeDe : « light », « dark », ou rien (le système décide). Réglé depuis le menu de l'édition.
func themeDe(r *http.Request) string {
	if c, err := r.Cookie("theme"); err == nil {
		switch c.Value {
		case "clair":
			return "light"
		case "sombre":
			return "dark"
		}
	}
	return ""
}

func (app *App) choisirTheme(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		app.erreur(w, r, http.StatusForbidden, "Ce geste se fait depuis Perches.")
		return
	}
	c := &http.Cookie{Name: "theme", Path: "/", MaxAge: 365 * 24 * 3600,
		Secure: strings.HasPrefix(app.baseURL, "https"), SameSite: http.SameSiteLaxMode}
	switch v := r.FormValue("theme"); v {
	case "clair", "sombre":
		c.Value = v
	default:
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
	retour := r.FormValue("retour")
	if !strings.HasPrefix(retour, "/") || strings.HasPrefix(retour, "//") {
		retour = "/"
	}
	http.Redirect(w, r, retour, http.StatusSeeOther)
}

// ---- pages de l'édition rangées derrière le menu ----

func (app *App) pageReglages(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	app.rendre(w, r, "reglages.html", map[string]any{
		"TitrePage": liste.Titre + " — réglages", "Liste": liste, "BaseURL": app.baseURL,
		"OK": r.URL.Query().Has("ok"),
	})
}

func (app *App) pageInviter(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		app.introuvable(w, r)
		return
	}
	app.rendre(w, r, "inviter.html", map[string]any{
		"TitrePage": liste.Titre + " — inviter", "Liste": liste, "BaseURL": app.baseURL,
		"Invitation": r.URL.Query().Get("invitation"),
	})
}

func (app *App) aPropos(w http.ResponseWriter, r *http.Request) {
	app.rendre(w, r, "apropos.html", map[string]any{"TitrePage": "À propos de Perches", "Politique": app.politique})
}

// tendrePerche : poser une perche sur un repéré — « j'y vais, si ça te dit » — avec ses dates
// (par défaut, celles de l'événement). Sur une perche déjà tendue, cela corrige ses dates :
// de la logistique, les e-mails connus sont prévenus.
func (app *App) tendrePerche(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	var finEvenement any
	if intention.Fin.Valid {
		finEvenement = intention.Fin.String
	}
	quand, fin, err := datesPercheDepuisForm(r, intention.Quand.String, finEvenement)
	if err != nil {
		app.erreur(w, r, http.StatusBadRequest, "Quand j'y vais : il faut une date valide, et une fin qui suit le début.")
		return
	}
	finTexte, _ := fin.(string)
	dejaQuand, dejaFin := intention.DatesPerche()
	change := intention.Tendue() && (quand != dejaQuand.String || finTexte != dejaFin.String)
	pq, pf := datesPropres(quand, fin, intention.Quand.String, finEvenement)
	if _, err := app.db.Exec(`UPDATE intentions SET perche_tendue_le = coalesce(perche_tendue_le, datetime('now')),
		perche_quand = ?, perche_fin = ? WHERE id = ?`, pq, pf, intention.ID); err != nil {
		app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
		return
	}
	if change && !intention.AnnuleeLe.Valid {
		intention.PercheQuand, intention.PercheFin = sqlString(quand), sqlString(finTexte)
		app.notifierLogistique(intention.ID, "« "+intention.Titre+" » change de date",
			fmt.Sprintf("« %s » : j'y vais finalement %s.\n\n%s/i/%s", intention.Titre, intention.PercheQuandFR(), app.baseURL, intention.Jeton))
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?ok=perche#perche-%d", liste.JetonEdition, intention.ID), http.StatusSeeOther)
}

// retirerPerche : l'hôte n'y va plus — l'événement reste repéré sur la page ; ceux qui ont laissé
// un e-mail sont prévenus (logistique) ; les réponses restent dans l'édition, comme des lettres.
func (app *App) confirmerRetraitPerche(w http.ResponseWriter, r *http.Request) {
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	app.confirmer(w, r, "Retirer la perche sur « "+intention.Titre+" » ?",
		prevenus(app.nbAvecEmail(intention.ID))+" L'événement reste sur ta page, repéré sans engagement ; les réponses reçues restent ici.",
		fmt.Sprintf("/e/%s/intentions/%d/retirer-perche", liste.JetonEdition, intention.ID), "Oui, retirer la perche", "/e/"+liste.JetonEdition)
}

func (app *App) retirerPerche(w http.ResponseWriter, r *http.Request) {
	if app.tropVite(w, r) {
		return
	}
	liste, intention, ok := app.intentionDuChemin(w, r)
	if !ok {
		return
	}
	if intention.Tendue() {
		if _, err := app.db.Exec(`UPDATE intentions SET perche_tendue_le = NULL, perche_quand = NULL, perche_fin = NULL WHERE id = ?`, intention.ID); err != nil {
			app.erreur(w, r, http.StatusInternalServerError, "Ça n'a pas pu être enregistré. Réessaie dans un instant.")
			return
		}
		if !intention.AnnuleeLe.Valid {
			app.notifierLogistique(intention.ID, "annulation : "+intention.Titre,
				fmt.Sprintf("« %s » (%s) : je n'y vais finalement pas.\n\n%s/i/%s", intention.Titre, intention.PercheQuandFR(), app.baseURL, intention.Jeton))
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/e/%s?ok=retiree#perche-%d", liste.JetonEdition, intention.ID), http.StatusSeeOther)
}
