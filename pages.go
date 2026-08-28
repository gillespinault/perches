package main

import (
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
	Ouverte     bool
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
		Ouverte:    !i.AnnuleeLe.Valid && !i.Passee() && !liste.FermeeLe.Valid,
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
