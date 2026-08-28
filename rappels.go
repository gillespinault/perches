package main

import (
	"fmt"
	"log"
	"time"
)

func (app *App) boucleRappels() {
	for {
		app.envoyerRappels()
		time.Sleep(time.Hour)
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
		WHERE r.email IS NOT NULL
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
		corps += fmt.Sprintf(".\n\n%s/i/%s\n\nSi finalement tu ne peux pas : rien à faire, le silence vaut « pas cette fois ».",
			app.baseURL, ra.jetonI)
		app.envoyer(ra.email, "Perches — demain : "+ra.titre, corps, "rappel_veille", ra.reponseID, 0)
	}
}
