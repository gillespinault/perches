package main

import (
	"fmt"
	"os"
	"strings"
)

// cheminSauvegarde : la copie vit à côté de la base (`/data/perches.db` →
// `/data/perches.sauvegarde.db`), dans le même volume — la sauvegarde de l'hôte
// (kopia, rsync…) n'a qu'à emporter le dossier.
func cheminSauvegarde(cheminDB string) string {
	return strings.TrimSuffix(cheminDB, ".db") + ".sauvegarde.db"
}

// sauvegarder écrit une copie cohérente de la base par VACUUM INTO : une seule
// transaction de lecture, donc jamais une écriture à moitié faite — ce qu'une copie
// brute du fichier pendant un commit ne garantit pas. Fichier temporaire puis
// renommage : la copie précédente reste intacte tant que la nouvelle n'est pas
// complète. Sans SQLite en ligne de commande dans le conteneur, c'est le binaire
// lui-même qui s'en charge, toutes les heures (voir boucleRappels).
func (app *App) sauvegarder() error {
	if app.cheminDB == "" {
		return nil
	}
	cible := cheminSauvegarde(app.cheminDB)
	tmp := cible + ".tmp"
	os.Remove(tmp) // VACUUM INTO refuse une cible existante
	if _, err := app.db.Exec(`VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("vacuum into : %w", err)
	}
	return os.Rename(tmp, cible)
}
