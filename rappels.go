package main

import (
	"fmt"
	"log"
	"time"
)

func (app *App) boucleRappels() {
	for {
		// Un rappel ne vibre pas à minuit : entre 8 h et 20 h locales seulement.
		if h := time.Now().Hour(); h >= 8 && h < 20 {
			app.envoyerRappels()
		}
		app.purgerEmails()
		time.Sleep(time.Hour)
	}
}

// purgerEmails : l'e-mail d'un invité n'a plus d'usage un mois après la date ; il est effacé,
// pas seulement masqué (décision 2026-08-28, vie privée des invités).
func (app *App) purgerEmails() {
	if _, err := app.db.Exec(`UPDATE reponses SET email = NULL WHERE email IS NOT NULL AND intention_id IN
		(SELECT id FROM intentions WHERE quand IS NOT NULL AND date(coalesce(fin, quand)) < date('now', '-30 days'))`); err != nil {
		log.Printf("purge e-mails : %v", err)
	}
}

// envoyerRappels : le rappel de la veille, pour les seules réponses qui ont laissé
// un e-mail. Idempotent grâce au journal `envois`. Convention 11 : c'est de la
// logistique, rien de social.
func (app *App) envoyerRappels() {
	rows, err := app.db.Query(`
		SELECT r.id, r.email, r.prenom, i.titre, i.quand, i.lieu, i.jeton
		FROM reponses r
		JOIN intentions i ON i.id = r.intention_id
		JOIN listes l ON l.id = i.liste_id
		WHERE r.email IS NOT NULL
		  AND l.fermee_le IS NULL
		  AND r.statut <> 'jaurais_aime'
		  AND i.annulee_le IS NULL
		  AND i.quand IS NOT NULL
		  AND date(i.quand) = date('now', 'localtime', '+1 day')
		  AND NOT EXISTS (SELECT 1 FROM envois e WHERE e.reponse_id = r.id AND e.type = 'rappel_veille')`)
	if err != nil {
		log.Printf("rappels : %v", err)
		return
	}
	defer rows.Close()
	type rappel struct {
		reponseID           int64
		email, prenom       string
		titre, lieu, jetonI string
		quand               string
	}
	var rappels []rappel
	for rows.Next() {
		var ra rappel
		if err := rows.Scan(&ra.reponseID, &ra.email, &ra.prenom, &ra.titre, &ra.quand, &ra.lieu, &ra.jetonI); err == nil {
			rappels = append(rappels, ra)
		}
	}
	rows.Close()
	for _, ra := range rappels {
		corps := fmt.Sprintf("Demain : %s — %s", ra.titre, quandFR(sqlString(ra.quand)))
		if ra.lieu != "" {
			corps += " — " + ra.lieu
		}
		corps += fmt.Sprintf(".\n\n%s/i/%s", app.baseURL, ra.jetonI)
		app.envoyer(ra.email, "Perches — demain : "+ra.titre, corps, "rappel_veille", ra.reponseID, 0)
	}
}
