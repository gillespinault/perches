package main

import (
	"fmt"
	"strings"
)

// slugDe dérive une adresse de page d'un titre : « Les perches de Léa » → « lea ».
// Accents retirés, minuscules, tirets ; le préfixe « les perches de » tombe quand
// il reste quelque chose derrière.
func slugDe(titre string) string {
	t := strings.ToLower(strings.TrimSpace(titre))
	t = sansPrefixe(t)
	var b strings.Builder
	dernierTiret := true // évite un tiret en tête
	for _, r := range t {
		if s, ok := sansAccent[r]; ok {
			r = s
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dernierTiret = false
		default:
			if !dernierTiret {
				b.WriteByte('-')
				dernierTiret = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "liste"
	}
	return s
}

// sansPrefixe retire « les perches de », « la liste d' », « perche de »… quand il reste
// quelque chose derrière.
func sansPrefixe(t string) string {
	for _, article := range []string{"les ", "la ", "le ", ""} {
		for _, nom := range []string{"perches", "perche", "liste"} {
			for _, de := range []string{" de ", " d'", " d’"} {
				p := article + nom + de
				if strings.HasPrefix(t, p) && strings.TrimSpace(t[len(p):]) != "" {
					return t[len(p):]
				}
			}
		}
	}
	return t
}

// slugLibre ajoute -2, -3… tant que l'adresse est prise.
func (app *App) slugLibre(base string) string {
	slug := base
	for n := 2; ; n++ {
		var k int
		app.db.QueryRow(`SELECT count(*) FROM listes WHERE slug = ?`, slug).Scan(&k)
		if k == 0 {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}

var sansAccent = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'ç': 'c', 'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i', 'ñ': 'n',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u', 'ý': 'y', 'ÿ': 'y',
	'œ': 'o', 'æ': 'a', 'ß': 's',
}
