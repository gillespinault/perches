package main

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// Un Markdown de lettre, pas de wiki : paragraphes, listes, **gras**, *italique*, [liens](https://…)
// et adresses http(s) nues. Tout est échappé d'abord ; rien d'autre ne passe (ni HTML, ni images,
// ni titres). Décision 2026-08-28.

var (
	mdGras     = regexp.MustCompile(`\*\*(.+?)\*\*`)
	mdItalique = regexp.MustCompile(`(^|[^\w*])\*([^*\n]+?)\*`)
	mdLien     = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
	mdAutolien = regexp.MustCompile(`(^|[\s(])(https?://[^\s<)]+)`)
)

func enLigne(s string) string {
	s = html.EscapeString(s)
	s = mdLien.ReplaceAllString(s, `<a href="$2" rel="noopener">$1</a>`)
	s = mdAutolien.ReplaceAllString(s, `$1<a href="$2" rel="noopener">$2</a>`)
	s = mdGras.ReplaceAllString(s, `<strong>$1</strong>`)
	s = mdItalique.ReplaceAllString(s, `$1<em>$2</em>`)
	return s
}

func rendreMarkdown(src string) template.HTML {
	src = strings.ReplaceAll(strings.TrimSpace(src), "\r\n", "\n")
	if src == "" {
		return ""
	}
	var b strings.Builder
	estItem := func(l string) bool { return strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") }
	for _, bloc := range regexp.MustCompile(`\n{2,}`).Split(src, -1) {
		lignes := strings.Split(strings.TrimSpace(bloc), "\n")
		// Un bloc peut mêler des lignes de texte et des items : on regroupe les items consécutifs.
		for k := 0; k < len(lignes); {
			if estItem(lignes[k]) {
				b.WriteString("<ul>")
				for ; k < len(lignes) && estItem(lignes[k]); k++ {
					b.WriteString("<li>" + enLigne(strings.TrimSpace(lignes[k][2:])) + "</li>")
				}
				b.WriteString("</ul>\n")
				continue
			}
			b.WriteString("<p>")
			for premiere := true; k < len(lignes) && !estItem(lignes[k]); k++ {
				if !premiere {
					b.WriteString("<br>")
				}
				b.WriteString(enLigne(lignes[k]))
				premiere = false
			}
			b.WriteString("</p>\n")
		}
	}
	return template.HTML(b.String())
}

// sansMarkdown : le texte nu, pour la carte de partage et l'extrait sur une perche.
func sansMarkdown(s string) string {
	s = mdLien.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "*", "", "\n- ", "\n", "\n* ", "\n").Replace(s)
	return strings.TrimPrefix(s, "- ")
}
