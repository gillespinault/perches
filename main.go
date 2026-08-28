package main

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"
)

//go:embed templates/*.html
var tplFS embed.FS

//go:embed static
var staticFS embed.FS

//go:embed schema.sql
var schemaSQL string

type App struct {
	db        *sql.DB
	mailer    Mailer
	tpl       *template.Template
	politique string // ouverte | invitation | fermee
	baseURL   string
	limiteur  *Limiteur
	// garde-fous (voir garde.go)
	derriereProxy bool   // croire X-Forwarded-For
	synchrone     bool   // courriels envoyés dans la requête (tests)
	csp           string // Content-Security-Policy calculée au démarrage
}

func (app *App) handler() http.Handler { return app.protege(app.routes()) }

func main() {
	nouveauCode := flag.Bool("nouveau-code", false, "génère un code d'invitation et sort")
	flag.Parse()

	db, err := ouvrirDB(env("PERCHES_DB", "data/perches.db"))
	if err != nil {
		log.Fatalf("base : %v", err)
	}

	if *nouveauCode {
		code := jeton(8)
		if _, err := db.Exec(`INSERT INTO codes_invitation (code) VALUES (?)`, code); err != nil {
			log.Fatalf("code : %v", err)
		}
		// Lien prêt à envoyer : l'accueil ne montre le formulaire qu'avec ce code.
		fmt.Println(env("PERCHES_BASE_URL", "http://localhost:8080") + "/?code=" + code)
		return
	}

	app := &App{
		db:            db,
		mailer:        mailerDepuisEnv(),
		tpl:           chargerTemplates(),
		politique:     env("PERCHES_POLITIQUE", "ouverte"),
		baseURL:       env("PERCHES_BASE_URL", "http://localhost:8080"),
		limiteur:      nouveauLimiteur(),
		derriereProxy: os.Getenv("PERCHES_DERRIERE_PROXY") != "",
		csp:           cspDepuisLayout(),
	}

	go app.boucleRappels()

	addr := env("PERCHES_ADDR", ":8080")
	log.Printf("perches écoute sur %s (politique : %s)", addr, app.politique)
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func (app *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.accueil)
	mux.HandleFunc("POST /listes", app.creerListe)
	mux.HandleFunc("POST /recuperation", app.recupererLien)
	mux.HandleFunc("POST /oublier", app.oublierAtelier)
	mux.HandleFunc("GET /l/{slug}", app.voirListe)
	mux.HandleFunc("GET /i/{jeton}", app.voirIntention)
	mux.HandleFunc("POST /i/{jeton}/reponses", app.repondre)
	mux.HandleFunc("GET /e/{jeton}", app.editerListe)
	mux.HandleFunc("POST /e/{jeton}", app.majListe)
	mux.HandleFunc("POST /e/{jeton}/reglages", app.reglages)
	mux.HandleFunc("POST /e/{jeton}/intentions", app.creerIntention)
	mux.HandleFunc("POST /e/{jeton}/intentions/{id}/annuler", app.annulerIntention)
	mux.HandleFunc("POST /e/{jeton}/intentions/{id}/maj", app.majIntention)
	mux.HandleFunc("POST /e/{jeton}/reponses/{id}/effacer", app.effacerReponse)
	mux.HandleFunc("GET /e/{jeton}/export.json", app.exportComplet)
	mux.HandleFunc("POST /e/{jeton}/invitations", app.creerInvitation)
	// confirmations et gestes rares (atelier.go)
	mux.HandleFunc("GET /e/{jeton}/intentions/{id}/annuler", app.confirmerAnnulation)
	mux.HandleFunc("POST /e/{jeton}/intentions/{id}/retablir", app.retablirIntention)
	mux.HandleFunc("GET /e/{jeton}/reponses/{id}/effacer", app.confirmerEffacement)
	mux.HandleFunc("GET /e/{jeton}/fermer", app.confirmerFermeture)
	mux.HandleFunc("POST /e/{jeton}/fermer", app.fermerListe)
	mux.HandleFunc("POST /e/{jeton}/rouvrir", app.rouvrirListe)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.HandleFunc("/", app.introuvable)
	return mux
}

func env(cle, defaut string) string {
	if v := os.Getenv(cle); v != "" {
		return v
	}
	return defaut
}

// jeton renvoie une chaîne hexadécimale aléatoire de 2×octets caractères.
func jeton(octets int) string {
	b := make([]byte, octets)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func chargerTemplates() *template.Template {
	return template.Must(template.New("").Funcs(fonctionsTpl).ParseFS(tplFS, "templates/*.html"))
}
