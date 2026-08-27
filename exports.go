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
	var b strings.Builder
	ligne := func(s string) { b.WriteString(s + "\r\n") }
	ligne("BEGIN:VCALENDAR")
	ligne("VERSION:2.0")
	ligne("PRODID:-//perches//FR")
	ligne("CALSCALE:GREGORIAN")
	ligne("X-WR-CALNAME:" + echapperICS(nom))
	horodatage := time.Now().UTC().Format("20060102T150405Z")
	for _, i := range intentions {
		if !i.Quand.Valid || i.AnnuleeLe.Valid {
			continue
		}
		t, avecHeure, err := analyserQuand(i.Quand.String)
		if err != nil {
			continue
		}
		ligne("BEGIN:VEVENT")
		ligne("UID:" + i.Jeton + "@perches")
		ligne("DTSTAMP:" + horodatage)
		if avecHeure {
			ligne("DTSTART:" + t.Format("20060102T150400"))
		} else {
			ligne("DTSTART;VALUE=DATE:" + t.Format("20060102"))
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
type intentionJSON struct {
	Jeton       string `json:"perche"`
	Titre       string `json:"titre"`
	Description string `json:"description,omitempty"`
	Quand       string `json:"quand,omitempty"`
	Lieu        string `json:"lieu,omitempty"`
	URLExterne  string `json:"url_externe,omitempty"`
	Capacite    int64  `json:"capacite,omitempty"`
	JyVais      bool   `json:"jy_vais_de_toute_facon"`
	Annulee     bool   `json:"annulee,omitempty"`
}

func versIntentionJSON(i Intention) intentionJSON {
	return intentionJSON{
		Jeton: i.Jeton, Titre: i.Titre, Description: i.Description,
		Quand: i.Quand.String, Lieu: i.Lieu, URLExterne: i.URLExterne.String,
		Capacite: i.Capacite.Int64, JyVais: i.JyVais, Annulee: i.AnnuleeLe.Valid,
	}
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
		Email         string `json:"email,omitempty"`
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
				PrenomVisible: rep.PrenomVisible, Email: rep.Email.String, CreeLe: rep.CreeLe,
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
