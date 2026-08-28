package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// L'image du site d'un événement (V0 shippable, point 1b). Quand l'hôte donne un lien, Perches
// va lire la page comme le ferait une messagerie, prend son `og:image`, en garde une copie
// réduite, et la sert lui-même : jamais de lien direct vers un tiers (la CSP l'interdit, et
// l'adresse des invités ne fuit nulle part). Le serveur va chercher une adresse donnée par un
// humain : on refuse les adresses internes, on borne le temps et la taille.

const (
	imageTailleMaxHTML  = 512 << 10 // la page : 512 Ko suffisent pour l'en-tête
	imageTailleMaxOctet = 6 << 20   // l'image d'origine : 6 Mo
	imageLargeurMax     = 1200
)

var (
	reMeta    = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	reAttr    = regexp.MustCompile(`(?is)([a-z:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	clesImage = []string{"og:image:secure_url", "og:image", "twitter:image", "twitter:image:src"}
)

// adressePublique : refuse le bouclage, les réseaux privés, les liens locaux — tout ce qu'un
// serveur ne doit pas aller chercher pour le compte d'un inconnu.
func adressePublique(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast())
}

// clientSortant : un client HTTP qui vérifie chaque adresse au moment de se connecter — y compris
// après une redirection — et n'attend pas longtemps.
func (app *App) clientSortant() *http.Client {
	d := &net.Dialer{Timeout: 5 * time.Second}
	if !app.imagesLocales {
		d.Control = func(network, address string, c syscall.RawConn) error {
			hote, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(hote); ip == nil || !adressePublique(ip) {
				return fmt.Errorf("adresse refusée : %s", hote)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second, DisableKeepAlives: true},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return errors.New("trop de redirections")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("schéma refusé")
			}
			return nil
		},
	}
}

func (app *App) lireBorne(u string, max int64, accepte func(string) bool) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Perches/1 (+"+app.baseURL+"; aperçu d'un lien donné par un hôte)")
	req.Header.Set("Accept-Language", "fr, en;q=0.5")
	resp, err := app.clientSortant().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("statut %d", resp.StatusCode)
	}
	typ := strings.ToLower(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	if !accepte(typ) {
		return nil, "", fmt.Errorf("type inattendu : %s", typ)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > max {
		return nil, "", errors.New("trop volumineux")
	}
	return b, resp.Request.URL.String(), nil
}

// imageDeLaPage : l'adresse de l'image qu'une page déclare pour ses aperçus.
func imageDeLaPage(html []byte, base *url.URL) string {
	trouve := map[string]string{}
	for _, m := range reMeta.FindAll(html, -1) {
		var cle, contenu string
		for _, a := range reAttr.FindAllSubmatch(m, -1) {
			nom := strings.ToLower(string(a[1]))
			val := string(a[2]) + string(a[3]) + string(a[4])
			switch nom {
			case "property", "name":
				cle = strings.ToLower(strings.TrimSpace(val))
			case "content":
				contenu = strings.TrimSpace(val)
			}
		}
		if _, deja := trouve[cle]; cle != "" && contenu != "" && !deja {
			trouve[cle] = contenu
		}
	}
	for _, k := range clesImage {
		if v := trouve[k]; v != "" {
			if u, err := base.Parse(v); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
				return u.String()
			}
		}
	}
	return ""
}

// recupererImageDuSite : la page, son image, réduite et réencodée en JPEG. Renvoie l'image,
// l'adresse d'origine de l'image, ou une erreur — jamais un panique, jamais d'attente longue.
func (app *App) recupererImageDuSite(page string) ([]byte, string, error) {
	base, err := url.Parse(page)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, "", errors.New("adresse invalide")
	}
	html, finale, err := app.lireBorne(page, imageTailleMaxHTML, func(t string) bool {
		return t == "text/html" || t == "application/xhtml+xml" || t == ""
	})
	if err != nil {
		return nil, "", err
	}
	if u, err := url.Parse(finale); err == nil {
		base = u
	}
	src := imageDeLaPage(html, base)
	if src == "" {
		return nil, "", errors.New("la page ne déclare pas d'image")
	}
	brut, _, err := app.lireBorne(src, imageTailleMaxOctet, func(t string) bool {
		return t == "image/jpeg" || t == "image/png" || t == "image/webp" || t == "" || t == "application/octet-stream"
	})
	if err != nil {
		return nil, src, err
	}
	img, _, err := image.Decode(bytes.NewReader(brut))
	if err != nil {
		return nil, src, err
	}
	return reduire(img), src, nil
}

// reduire : au plus 1200 px de large, JPEG de qualité honnête — quelques dizaines de Ko.
func reduire(img image.Image) []byte {
	b := img.Bounds()
	l, h := b.Dx(), b.Dy()
	if l > imageLargeurMax {
		h = h * imageLargeurMax / l
		l = imageLargeurMax
	}
	dst := image.NewRGBA(image.Rect(0, 0, l, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82})
	return buf.Bytes()
}

// chercherImage : en arrière-plan, pour un événement dont le lien vient d'être donné ou changé.
// Un échec ne se voit pas : l'événement reste simplement sans image.
func (app *App) chercherImage(intentionID int64, page string) {
	if page == "" {
		return
	}
	app.enFond(func() {
		b, src, err := app.recupererImageDuSite(page)
		if err != nil {
			log.Printf("image du site (%s) : %v", page, err)
			return
		}
		if _, err := app.db.Exec(`UPDATE intentions SET image = ?, image_source = ? WHERE id = ? AND image_refusee = 0`, b, src, intentionID); err != nil {
			log.Printf("image du site : %v", err)
		}
	})
}

// couvrir : l'image redimensionnée pour remplir exactement l × h, recadrée au centre.
func couvrir(img image.Image, l, h int) *image.RGBA {
	b := img.Bounds()
	sl, sh := float64(b.Dx()), float64(b.Dy())
	echelle := float64(l) / sl
	if float64(h)/sh > echelle {
		echelle = float64(h) / sh
	}
	nl, nh := int(sl*echelle+.5), int(sh*echelle+.5)
	grand := image.NewRGBA(image.Rect(0, 0, nl, nh))
	draw.CatmullRom.Scale(grand, grand.Bounds(), img, b, draw.Src, nil)
	dst := image.NewRGBA(image.Rect(0, 0, l, h))
	draw.Draw(dst, dst.Bounds(), grand, image.Point{(nl - l) / 2, (nh - h) / 2}, draw.Src)
	return dst
}
