package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func sqlString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// ---- ICS ----

func echapperICS(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n", "\r", "")
	return r.Replace(s)
}

func servirICS(w http.ResponseWriter, nom string, intentions []Intention, baseURL string) {
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="`+slugDe(nom)+`.ics"`)
	var b strings.Builder
	ligne := func(s string) { b.WriteString(s + "\r\n") }
	ligne("BEGIN:VCALENDAR")
	ligne("VERSION:2.0")
	ligne("PRODID:-//perches//FR")
	ligne("CALSCALE:GREGORIAN")
	ligne("X-WR-CALNAME:" + echapperICS(nom))
	horodatage := time.Now().UTC().Format("20060102T150405Z")
	for _, i := range intentions {
		if !i.Quand.Valid {
			continue
		}
		// l'agenda d'un invité reçoit les dates de la perche — quand l'hôte y va — sinon celles de l'événement
		quand, finPerche := i.DatesAffichees()
		t, avecHeure, err := analyserQuand(quand.String)
		if err != nil {
			continue
		}
		ligne("BEGIN:VEVENT")
		ligne("UID:" + i.Jeton + "@perches")
		ligne("DTSTAMP:" + horodatage)
		switch fin, _, errFin := analyserQuand(finPerche.String); {
		case finPerche.Valid && errFin == nil:
			// plusieurs jours : journées entières, DTEND exclusif (le lendemain du dernier jour)
			ligne("DTSTART;VALUE=DATE:" + t.Format("20060102"))
			ligne("DTEND;VALUE=DATE:" + fin.AddDate(0, 0, 1).Format("20060102"))
		case avecHeure:
			ligne("DTSTART:" + t.Format("20060102T150400"))
			ligne("DURATION:PT2H")
		default:
			ligne("DTSTART;VALUE=DATE:" + t.Format("20060102"))
		}
		if i.AnnuleeLe.Valid {
			// l'agenda de l'invité retire l'événement : c'est le vrai service d'une annulation
			ligne("STATUS:CANCELLED")
		}
		ligne("SUMMARY:" + echapperICS(i.Titre))
		if i.Lieu != "" {
			ligne("LOCATION:" + echapperICS(i.Lieu))
		}
		desc := i.Description
		if i.URLExterne.Valid {
			desc = strings.TrimSpace(desc + "\n" + i.URLExterne.String)
		}
		if desc != "" {
			ligne("DESCRIPTION:" + echapperICS(desc))
		}
		ligne("URL:" + baseURL + "/i/" + i.Jeton)
		ligne("END:VEVENT")
	}
	ligne("END:VCALENDAR")
	fmt.Fprint(w, b.String())
}

// ---- JSON ----

// Le format ouvert (§8 du doc projet) : une intention est un objet JSON minuscule.
// La perche : le geste de l'hôte sur un repéré, avec ses dates. Absente = repéré seul.
type percheJSON struct {
	TendueLe string `json:"tendue_le"`
	Quand    string `json:"quand,omitempty"`
	Fin      string `json:"fin,omitempty"`
}

type intentionJSON struct {
	Jeton       string      `json:"perche"`
	Titre       string      `json:"titre"`
	Description string      `json:"description,omitempty"`
	Quand       string      `json:"quand,omitempty"`
	Fin         string      `json:"fin,omitempty"`
	Perche      *percheJSON `json:"perche_tendue,omitempty"` // le jeton s'appelle déjà « perche » : c'est le lien
	Lieu        string      `json:"lieu,omitempty"`
	URLExterne  string      `json:"url_externe,omitempty"`
	JyVais      bool        `json:"jy_vais_de_toute_facon"`
	Annulee     bool        `json:"annulee,omitempty"`
}

func versIntentionJSON(i Intention) intentionJSON {
	j := intentionJSON{
		Jeton: i.Jeton, Titre: i.Titre, Description: i.Description,
		Quand: i.Quand.String, Fin: i.Fin.String, Lieu: i.Lieu, URLExterne: i.URLExterne.String,
		JyVais: i.JyVais && i.Tendue(), Annulee: i.AnnuleeLe.Valid,
	}
	if i.Tendue() {
		q, f := i.DatesPerche()
		j.Perche = &percheJSON{TendueLe: i.PercheTendueLe.String, Quand: q.String, Fin: f.String}
	}
	return j
}

func servirJSONPublic(w http.ResponseWriter, liste *Liste, intentions []Intention) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sortie := map[string]any{
		"liste": map[string]string{
			"slug": liste.Slug, "titre": liste.Titre, "lettre": liste.Lettre, "etat": liste.Etat,
		},
	}
	items := []intentionJSON{}
	for _, i := range intentions {
		items = append(items, versIntentionJSON(i))
	}
	sortie["intentions"] = items
	json.NewEncoder(w).Encode(sortie)
}

// exportComplet : la portabilité promise à l'hôte — tout, réponses comprises.
func (app *App) exportComplet(w http.ResponseWriter, r *http.Request) {
	liste, err := app.listeParJetonEdition(r.PathValue("jeton"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	intentions, err := app.toutesIntentions(liste.ID)
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	type reponseJSON struct {
		Prenom        string `json:"prenom"`
		Statut        string `json:"statut"`
		Mot           string `json:"mot,omitempty"`
		PrenomVisible bool   `json:"prenom_visible"`
		CreeLe        string `json:"cree_le"`
	}
	type intentionComplete struct {
		intentionJSON
		Reponses []reponseJSON `json:"reponses"`
	}
	items := []intentionComplete{}
	for k := range intentions {
		app.chargerReponses(&intentions[k])
		ic := intentionComplete{intentionJSON: versIntentionJSON(intentions[k]), Reponses: []reponseJSON{}}
		for _, rep := range intentions[k].Reponses {
			ic.Reponses = append(ic.Reponses, reponseJSON{
				Prenom: rep.Prenom, Statut: rep.Statut, Mot: rep.Mot,
				PrenomVisible: rep.PrenomVisible, CreeLe: rep.CreeLe,
			})
		}
		items = append(items, ic)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"liste": map[string]string{
			"slug": liste.Slug, "titre": liste.Titre, "lettre": liste.Lettre,
			"etat": liste.Etat, "email": liste.Email.String,
		},
		"intentions": items,
	})
}
