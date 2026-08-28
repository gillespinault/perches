package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// La carte de partage — l'image que WhatsApp, Signal, Facebook ou Messages montrent sous un
// lien de Perches. Une par liste et par événement, dessinée ici, sans navigateur : le titre, une
// phrase, le bâton. Même typographie que la page (Newsreader pour la voix, Instrument Sans pour
// l'outil), en 1200 × 630, le format que toutes les messageries acceptent.

//go:embed polices/newsreader.ttf polices/instrument-sans.ttf
var policesFS embed.FS

const (
	carteLargeur = 1200
	carteHauteur = 630
	carteMarge   = 72
)

var (
	teintePapier = color.RGBA{0xf3, 0xf3, 0xef, 0xff}
	teinteEncre  = color.RGBA{0x17, 0x19, 0x1c, 0xff}
	teinteDoux   = color.RGBA{0x6c, 0x66, 0x5e, 0xff}
	teinteBois   = color.RGBA{0x6a, 0x4b, 0x2c, 0xff}
)

// Carte : ce qu'il y a à dire en une image.
type Carte struct {
	Titre   string // le titre de la liste ou de l'événement
	Sous    string // la première phrase de la lettre, ou les dates et le lieu
	Voix    bool   // Sous est la voix de l'hôte (serif) plutôt qu'un fait (sans)
	Perche  string // « J'y vais le 24 oct. » — vide si aucune perche n'est tendue
	Pied    string // l'adresse de l'instance
	Compact bool   // titre plus petit d'emblée (page d'une liste : la lettre a la place)
}

var (
	policesUneFois sync.Once
	policeSerif    *opentype.Font
	policeSans     *opentype.Font
	policesErr     error
)

func chargerPolices() error {
	policesUneFois.Do(func() {
		var b []byte
		if b, policesErr = policesFS.ReadFile("polices/newsreader.ttf"); policesErr != nil {
			return
		}
		if policeSerif, policesErr = opentype.Parse(b); policesErr != nil {
			return
		}
		if b, policesErr = policesFS.ReadFile("polices/instrument-sans.ttf"); policesErr != nil {
			return
		}
		policeSans, policesErr = opentype.Parse(b)
	})
	return policesErr
}

func face(f *opentype.Font, taille float64) font.Face {
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: taille, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	return fc
}

// couperEnLignes : retour à la ligne au mot, dans une largeur donnée ; au-delà de max lignes,
// la dernière est tronquée avec des points de suspension.
func couperEnLignes(d *font.Drawer, texte string, largeur, max int) []string {
	var lignes []string
	var ligne string
	for _, mot := range strings.Fields(texte) {
		essai := mot
		if ligne != "" {
			essai = ligne + " " + mot
		}
		if d.MeasureString(essai).Ceil() <= largeur || ligne == "" {
			ligne = essai
			continue
		}
		lignes = append(lignes, ligne)
		ligne = mot
	}
	if ligne != "" {
		lignes = append(lignes, ligne)
	}
	if len(lignes) > max {
		lignes = lignes[:max]
		derniere := lignes[max-1]
		for d.MeasureString(derniere+"…").Ceil() > largeur && strings.Contains(derniere, " ") {
			derniere = derniere[:strings.LastIndex(derniere, " ")]
		}
		lignes[max-1] = derniere + "…"
	}
	return lignes
}

func ecrire(img draw.Image, f font.Face, teinte color.Color, x, y int, s string) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(teinte), Face: f, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// baton : le logo — un carré arrondi couleur bois, un bâton clair dessus — à la taille voulue.
// Les traits sont des disques posés le long des courbes : le rasteriseur ne sait que remplir.
func baton(img draw.Image, x, y, taille int) {
	r := vector.NewRasterizer(taille, taille)
	r.DrawOp = draw.Over
	s := float32(taille) / 64
	// carré arrondi (rayon 14/64)
	rd := 14 * s
	w := float32(taille)
	r.MoveTo(rd, 0)
	r.LineTo(w-rd, 0)
	r.CubeTo(w-rd*0.45, 0, w, rd*0.45, w, rd)
	r.LineTo(w, w-rd)
	r.CubeTo(w, w-rd*0.45, w-rd*0.45, w, w-rd, w)
	r.LineTo(rd, w)
	r.CubeTo(rd*0.45, w, 0, w-rd*0.45, 0, w-rd)
	r.LineTo(0, rd)
	r.CubeTo(0, rd*0.45, rd*0.45, 0, rd, 0)
	r.ClosePath()
	r.Draw(img, image.Rect(x, y, x+taille, y+taille), image.NewUniform(teinteBois), image.Point{})

	r2 := vector.NewRasterizer(taille, taille)
	r2.DrawOp = draw.Over
	disque := func(cx, cy, ray float32) {
		const k = 0.5523
		r2.MoveTo(cx+ray, cy)
		r2.CubeTo(cx+ray, cy+ray*k, cx+ray*k, cy+ray, cx, cy+ray)
		r2.CubeTo(cx-ray*k, cy+ray, cx-ray, cy+ray*k, cx-ray, cy)
		r2.CubeTo(cx-ray, cy-ray*k, cx-ray*k, cy-ray, cx, cy-ray)
		r2.CubeTo(cx+ray*k, cy-ray, cx+ray, cy-ray*k, cx+ray, cy)
		r2.ClosePath()
	}
	courbe := func(x0, y0, x1, y1, x2, y2, x3, y3, epaisseur float32) {
		for i := 0; i <= 40; i++ {
			t := float32(i) / 40
			u := 1 - t
			px := u*u*u*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3
			py := u*u*u*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3
			disque(px*s, py*s, epaisseur*s/2)
		}
	}
	courbe(13, 51, 19, 43, 25, 36.5, 33, 29.5, 7.5)
	courbe(31.5, 30.8, 38, 25, 45, 18.5, 51.5, 13.5, 5)
	courbe(37.5, 25.5, 39.3, 24.5, 41.2, 23.5, 43, 22.5, 3.6)
	r2.Draw(img, image.Rect(x, y, x+taille, y+taille), image.NewUniform(teintePapier), image.Point{})
}

// dessinerCarte : l'image PNG d'une carte.
func dessinerCarte(c Carte) ([]byte, error) {
	if err := chargerPolices(); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, carteLargeur, carteHauteur))
	draw.Draw(img, img.Bounds(), image.NewUniform(teintePapier), image.Point{}, draw.Src)
	largeur := carteLargeur - 2*carteMarge

	// la marque, en haut
	baton(img, carteMarge, 58, 40)
	ecrire(img, face(policeSans, 22), teinteDoux, carteMarge+56, 87, "P E R C H E S")

	// le titre : grand, puis moins grand s'il déborde
	tailles := []float64{80, 66, 54}
	if c.Compact {
		tailles = []float64{66, 54, 46}
	}
	var titre []string
	var fTitre font.Face
	for _, t := range tailles {
		fTitre = face(policeSerif, t)
		titre = couperEnLignes(&font.Drawer{Face: fTitre}, c.Titre, largeur, 3)
		if len(titre) <= 2 {
			break
		}
	}
	interligne := fTitre.Metrics().Height.Ceil() * 108 / 100
	y := 150 + fTitre.Metrics().Ascent.Ceil()
	for _, l := range titre {
		ecrire(img, fTitre, teinteEncre, carteMarge, y, l)
		y += interligne
	}
	y += 18

	// la phrase : la voix de l'hôte en serif, un fait en sans
	if c.Sous != "" {
		var fSous font.Face
		teinte := teinteDoux
		if c.Voix {
			fSous = face(policeSerif, 34)
		} else {
			fSous = face(policeSans, 30)
			teinte = teinteEncre
		}
		y += fSous.Metrics().Ascent.Ceil()
		for _, l := range couperEnLignes(&font.Drawer{Face: fSous}, c.Sous, largeur, 2) {
			ecrire(img, fSous, teinte, carteMarge, y, l)
			y += fSous.Metrics().Height.Ceil() * 115 / 100
		}
		y += 10
	}

	// la perche, s'il y en a une : le bâton et « J'y vais… »
	if c.Perche != "" {
		fP := face(policeSans, 30)
		y += fP.Metrics().Ascent.Ceil()
		baton(img, carteMarge, y-30, 34)
		ecrire(img, fP, teinteEncre, carteMarge+48, y, c.Perche)
	}

	// le pied
	fPied := face(policeSans, 22)
	ecrire(img, fPied, teinteDoux, carteMarge, carteHauteur-56, "des intentions, sans obligation.")
	if c.Pied != "" {
		l := (&font.Drawer{Face: fPied}).MeasureString(c.Pied).Ceil()
		ecrire(img, fPied, teinteDoux, carteLargeur-carteMarge-l, carteHauteur-56, c.Pied)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// empreinteCarte : un court identifiant du contenu, dans l'URL de l'image — les messageries
// gardent les aperçus longtemps ; changer le titre change l'adresse.
func empreinteCarte(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:4])
}

// carteDeListe et carteDIntention : ce que dit chaque carte.
func (app *App) carteDeListe(liste *Liste, nbPerches int) Carte {
	sous := descriptionOG(sansMarkdown(liste.Lettre), nbPerches)
	return Carte{Titre: liste.Titre, Sous: sous, Voix: true, Pied: hoteDeBase(app.baseURL), Compact: true}
}

func (app *App) carteDIntention(i *Intention) Carte {
	sous := i.QuandFR()
	if i.Lieu != "" {
		sous += " · " + i.Lieu
	}
	c := Carte{Titre: i.Titre, Sous: sous, Pied: hoteDeBase(app.baseURL)}
	if i.Tendue() && !i.AnnuleeLe.Valid {
		c.Perche = "J'y vais"
		if i.PercheADater() {
			c.Perche += " " + i.PercheCourtFR()
		}
		c.Perche += " — si ça te dit"
	}
	return c
}

// hoteDeBase : « perches.robotsinlove.be » depuis l'URL de base.
func hoteDeBase(base string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	return strings.TrimSuffix(s, "/")
}
