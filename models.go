package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"strings"
	"sync"
	"time"
)

type Liste struct {
	ID           int64
	Slug         string
	JetonEdition string
	Titre        string
	Lettre       string
	Etat         string
	Email        sql.NullString
	FermeeLe     sql.NullString
	CreeLe       string
}

type Intention struct {
	ID          int64
	ListeID     int64
	Jeton       string
	Titre       string
	Description string
	Quand       sql.NullString
	Fin         sql.NullString // dernier jour, si la perche dure plusieurs jours (décision 2026-08-28)
	Echeance    sql.NullString
	Lieu        string
	URLExterne  sql.NullString
	JyVais      bool
	Visibilite  string
	AnnuleeLe   sql.NullString
	CreeLe      string

	Reponses   []Reponse
	NbJySerai  int
	NbPeutEtre int
}

type Reponse struct {
	ID            int64
	IntentionID   int64
	Prenom        string
	Statut        string
	Mot           string
	PrenomVisible bool
	Email         sql.NullString
	CreeLe        string
}

// ---- temps ----

func analyserQuand(q string) (t time.Time, avecHeure bool, err error) {
	if t, err = time.ParseInLocation("2006-01-02T15:04", q, time.Local); err == nil {
		return t, true, nil
	}
	t, err = time.ParseInLocation("2006-01-02", q, time.Local)
	return t, false, err
}

// dernierJour : le jour où la perche se termine — la date de fin si elle dure, sinon le jour même.
func (i *Intention) dernierJour() (t time.Time, avecHeure bool, ok bool) {
	if !i.Quand.Valid {
		return t, false, false
	}
	if i.Fin.Valid {
		if f, _, err := analyserQuand(i.Fin.String); err == nil {
			return f, false, true
		}
	}
	t, avecHeure, err := analyserQuand(i.Quand.String)
	return t, avecHeure, err == nil
}

func (i *Intention) Passee() bool {
	t, avecHeure, ok := i.dernierJour()
	if !ok {
		return false
	}
	if avecHeure {
		t = t.Add(2 * time.Hour) // petite marge : on peut encore se joindre en cours
	} else {
		t = t.Add(24 * time.Hour) // passée à la fin du jour
	}
	return time.Now().After(t)
}

// ReponsesEffacees : effacement public des réponses, 30 jours après le dernier jour.
func (i *Intention) ReponsesEffacees() bool {
	t, _, ok := i.dernierJour()
	if !ok {
		return false
	}
	return time.Now().After(t.Add(30 * 24 * time.Hour))
}

// QuandFR : la date telle que la page la dit — « du lundi 28 septembre au vendredi 2 octobre »
// si la perche dure plusieurs jours, sinon la date seule, relative quand elle est proche.
func (i *Intention) QuandFR() string {
	if !i.Fin.Valid || !i.Quand.Valid {
		return quandFR(i.Quand)
	}
	debut, _, err := analyserQuand(i.Quand.String)
	fin, _, err2 := analyserQuand(i.Fin.String)
	if err != nil || err2 != nil {
		return quandFR(i.Quand)
	}
	return "du " + dateFR(debut, fin.Year() != debut.Year()) + " au " + dateFR(fin, true)
}

var moisCourtFR = [...]string{"janv.", "févr.", "mars", "avr.", "mai", "juin",
	"juil.", "août", "sept.", "oct.", "nov.", "déc."}
var joursCourtFR = [...]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}

// JourCourt : « 28 sept. » — la colonne de gauche d'une liste de perches.
func (i *Intention) JourCourt() string {
	t, _, err := analyserQuand(i.Quand.String)
	if !i.Quand.Valid || err != nil {
		return "à fixer"
	}
	s := fmt.Sprintf("%d %s", t.Day(), moisCourtFR[t.Month()-1])
	if t.Year() != time.Now().Year() {
		s += fmt.Sprintf(" %d", t.Year())
	}
	return s
}

// Complement : sous le jour — « → 2 oct. » si la perche dure, sinon « demain », ou le jour de
// la semaine et l'heure (« sam. 10h00 »).
func (i *Intention) Complement() string {
	t, avecHeure, err := analyserQuand(i.Quand.String)
	if !i.Quand.Valid || err != nil {
		return ""
	}
	if i.Fin.Valid {
		if f, _, err := analyserQuand(i.Fin.String); err == nil {
			return fmt.Sprintf("→ %d %s", f.Day(), moisCourtFR[f.Month()-1])
		}
	}
	jour := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.Local) }
	var s string
	switch int(jour(t).Sub(jour(time.Now())).Hours() / 24) {
	case 0:
		s = "aujourd'hui"
	case 1:
		s = "demain"
	default:
		s = joursCourtFR[int(t.Weekday())]
	}
	if avecHeure {
		s += fmt.Sprintf(" %dh%02d", t.Hour(), t.Minute())
	}
	return s
}

// dateFR : « lundi 28 septembre », l'année seulement si elle n'est pas celle en cours.
func dateFR(t time.Time, avecAnnee bool) string {
	s := fmt.Sprintf("%s %d %s", joursFR[int(t.Weekday())], t.Day(), moisFR[t.Month()-1])
	if avecAnnee && t.Year() != time.Now().Year() {
		s += fmt.Sprintf(" %d", t.Year())
	}
	return s
}

var joursFR = [...]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
var moisFR = [...]string{"janvier", "février", "mars", "avril", "mai", "juin",
	"juillet", "août", "septembre", "octobre", "novembre", "décembre"}

func quandFR(q sql.NullString) string {
	if !q.Valid || q.String == "" {
		return "à fixer"
	}
	t, avecHeure, err := analyserQuand(q.String)
	if err != nil {
		return q.String
	}
	// Proche : dit en jours ; loin : la date, l'année seulement si elle change.
	aujourdhui := time.Now()
	jour := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.Local) }
	ecart := int(jour(t).Sub(jour(aujourdhui)).Hours() / 24)
	s := fmt.Sprintf("%s %d %s", joursFR[int(t.Weekday())], t.Day(), moisFR[t.Month()-1])
	if t.Year() != aujourdhui.Year() {
		s += fmt.Sprintf(" %d", t.Year())
	}
	switch {
	case ecart == 0 && avecHeure && t.Hour() >= 17:
		s = "ce soir"
	case ecart == 0:
		s = "aujourd'hui"
	case ecart == 1:
		s = "demain, " + s
	case ecart > 1 && ecart < 7:
		s = fmt.Sprintf("dans %d jours — %s", ecart, s)
	}
	if avecHeure {
		s += fmt.Sprintf(" à %dh%02d", t.Hour(), t.Minute())
	}
	return s
}

var fonctionsTpl = template.FuncMap{
	"quandfr": quandFR,
	"joindre": strings.Join,
	"md":      rendreMarkdown,
	"datede": func(q sql.NullString) string {
		if !q.Valid {
			return ""
		}
		return q.String[:min(10, len(q.String))]
	},
	"heurede": func(q sql.NullString) string {
		if !q.Valid || len(q.String) < 16 {
			return ""
		}
		return q.String[11:16]
	},
}

// ---- requêtes ----

const colonnesIntention = `id, liste_id, jeton, titre, description, quand, fin, echeance_decision,
	lieu, url_externe, jy_vais_de_toute_facon, visibilite, annulee_le, cree_le`

type scanneur interface{ Scan(dest ...any) error }

func scanIntention(row scanneur) (*Intention, error) {
	var i Intention
	err := row.Scan(&i.ID, &i.ListeID, &i.Jeton, &i.Titre, &i.Description, &i.Quand, &i.Fin, &i.Echeance,
		&i.Lieu, &i.URLExterne, &i.JyVais, &i.Visibilite, &i.AnnuleeLe, &i.CreeLe)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

const colonnesListe = `id, slug, jeton_edition, titre, lettre, etat, email, fermee_le, cree_le`

func scanListe(row scanneur) (*Liste, error) {
	var l Liste
	err := row.Scan(&l.ID, &l.Slug, &l.JetonEdition, &l.Titre, &l.Lettre, &l.Etat, &l.Email, &l.FermeeLe, &l.CreeLe)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (app *App) listeParSlug(slug string) (*Liste, error) {
	return scanListe(app.db.QueryRow(`SELECT `+colonnesListe+` FROM listes WHERE slug = ?`, slug))
}

func (app *App) listeParJetonEdition(j string) (*Liste, error) {
	return scanListe(app.db.QueryRow(`SELECT `+colonnesListe+` FROM listes WHERE jeton_edition = ?`, j))
}

func (app *App) intentionParJeton(j string) (*Intention, *Liste, error) {
	i, err := scanIntention(app.db.QueryRow(`SELECT `+colonnesIntention+` FROM intentions WHERE jeton = ?`, j))
	if err != nil {
		return nil, nil, err
	}
	l, err := scanListe(app.db.QueryRow(`SELECT `+colonnesListe+` FROM listes WHERE id = ?`, i.ListeID))
	if err != nil {
		return nil, nil, err
	}
	return i, l, nil
}

func (app *App) intentionDeListe(listeID, id int64) (*Intention, error) {
	return scanIntention(app.db.QueryRow(
		`SELECT `+colonnesIntention+` FROM intentions WHERE id = ? AND liste_id = ?`, id, listeID))
}

// intentionsPubliques : ce que montre la page publique — visibles, non annulées, à venir
// ou en cours (une perche de plusieurs jours reste sur la page jusqu'à son dernier jour).
func (app *App) intentionsPubliques(listeID int64) ([]Intention, error) {
	return app.requeteIntentions(`SELECT `+colonnesIntention+` FROM intentions
		WHERE liste_id = ? AND visibilite = 'page' AND annulee_le IS NULL
		  AND quand IS NOT NULL AND date(coalesce(fin, quand)) >= date('now','localtime')
		ORDER BY quand`, listeID)
}

func (app *App) toutesIntentions(listeID int64) ([]Intention, error) {
	return app.requeteIntentions(`SELECT `+colonnesIntention+` FROM intentions
		WHERE liste_id = ? ORDER BY quand`, listeID)
}

func (app *App) requeteIntentions(req string, args ...any) ([]Intention, error) {
	rows, err := app.db.Query(req, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Intention
	for rows.Next() {
		i, err := scanIntention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (app *App) chargerReponses(i *Intention) error {
	rows, err := app.db.Query(`SELECT id, intention_id, prenom, statut, mot, prenom_visible, email, cree_le
		FROM reponses WHERE intention_id = ? ORDER BY cree_le`, i.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	i.Reponses = nil
	i.NbJySerai, i.NbPeutEtre = 0, 0
	for rows.Next() {
		var r Reponse
		if err := rows.Scan(&r.ID, &r.IntentionID, &r.Prenom, &r.Statut, &r.Mot,
			&r.PrenomVisible, &r.Email, &r.CreeLe); err != nil {
			return err
		}
		switch r.Statut {
		case "jy_serai":
			i.NbJySerai++
		case "peut_etre":
			i.NbPeutEtre++
		}
		i.Reponses = append(i.Reponses, r)
	}
	return rows.Err()
}

// ---- rate limiting minimal (anti-abus, décision 2026-08-27) ----

type Limiteur struct {
	mu      sync.Mutex
	frappes map[string][]time.Time
}

func nouveauLimiteur() *Limiteur {
	return &Limiteur{frappes: map[string][]time.Time{}}
}

func (l *Limiteur) Autorise(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	seuil := time.Now().Add(-time.Minute)
	recentes := l.frappes[ip][:0]
	for _, t := range l.frappes[ip] {
		if t.After(seuil) {
			recentes = append(recentes, t)
		}
	}
	if len(recentes) >= 15 {
		l.frappes[ip] = recentes
		return false
	}
	l.frappes[ip] = append(recentes, time.Now())
	return true
}
