package main

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
)

// Garde-fous HTTP, dans l'esprit « pile ennuyeuse » : en-têtes de sécurité, corps borné,
// formulaire lu une fois. Tout ce qui suit s'applique avant les handlers.

const tailleMaxCorps = 64 << 10 // 64 Ko : un formulaire de Perches en fait quelques centaines d'octets

func (app *App) protege(suivant http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", app.csp)
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, tailleMaxCorps)
			if err := r.ParseForm(); err != nil {
				var trop *http.MaxBytesError
				if errors.As(err, &trop) {
					app.erreur(w, r, http.StatusRequestEntityTooLarge, "C'est trop long pour un formulaire de Perches.")
				} else {
					app.erreur(w, r, http.StatusBadRequest, "Formulaire illisible.")
				}
				return
			}
		}
		suivant.ServeHTTP(w, r)
	})
}

// cspDepuisLayout : la seule touche de script est dans layout.html, statique ; on l'autorise
// par son empreinte, rien d'autre.
func cspDepuisLayout() string {
	src, _ := tplFS.ReadFile("templates/layout.html")
	s := string(src)
	scripts := ""
	for {
		i := strings.Index(s, "<script>")
		if i < 0 {
			break
		}
		s = s[i+len("<script>"):]
		j := strings.Index(s, "</script>")
		if j < 0 {
			break
		}
		somme := sha256.Sum256([]byte(s[:j]))
		scripts += " 'sha256-" + base64.StdEncoding.EncodeToString(somme[:]) + "'"
		s = s[j:]
	}
	return "default-src 'self'; script-src" + scripts + "; style-src 'self'; img-src 'self' data:; " +
		"form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
}

// ipDe : l'adresse du client. X-Forwarded-For n'est cru que derrière un mandataire déclaré
// (PERCHES_DERRIERE_PROXY) — sinon n'importe qui contournerait le limiteur en l'inventant.
func (app *App) ipDe(r *http.Request) string {
	if app.derriereProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		}
	}
	hote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return hote
}

// borne : vrai si la chaîne tient dans n caractères.
func borne(s string, n int) bool { return len([]rune(s)) <= n }

// emailPlausible : une adresse à laquelle un courriel peut partir ; sinon chaîne vide.
func emailPlausible(s string) string {
	s = strings.TrimSpace(s)
	at := strings.LastIndex(s, "@")
	if at < 1 || !strings.Contains(s[at:], ".") || strings.ContainsAny(s, " \r\n") || !borne(s, 254) {
		return ""
	}
	return s
}

// urlPlausible : http(s) seulement — les agendas des invités reçoivent cette URL telle quelle.
func urlPlausible(s string) string {
	s = strings.TrimSpace(s)
	if (!strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://")) || !borne(s, 500) {
		return ""
	}
	return s
}

// enFond : les courriels partent hors de la requête (un changement de date sur une perche
// très suivie ne doit pas bloquer l'hôte) ; les tests restent synchrones.
func (app *App) enFond(f func()) {
	if app.synchrone {
		f()
		return
	}
	go f()
}
