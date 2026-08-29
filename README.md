# Perches

Perches sert à dire à ses amis « j'ai l'intention de faire ceci, à tel moment ; si
quelqu'un veut venir, tant mieux ». Une page par personne, une liste d'événements
datés ; les amis y répondent avec un prénom, sans compte ni application, et personne
n'est tenu de répondre.

En service depuis août 2026 sur perches.robotsinlove.be (v0, sur invitation). Maintenu
par intermittence.

## Comment ça marche

**Une liste** est une page publique : une introduction écrite par l'hôte, puis une
chronologie d'événements *repérés* (titre, dates, lieu, lien, quelques mots). Un
repéré ne demande rien aux lecteurs et n'a pas de formulaire de réponse.

**Une perche** est posée sur un repéré quand l'hôte y va : « j'y vais, si ça te dit »,
avec ses propres dates s'il n'y va qu'un jour sur cinq. Ce sont ces dates que
reçoivent les invités, dans la chronologie, dans le rappel de la veille et dans le
fichier calendrier. La perche se retire sans que l'événement quitte la page. Un filtre
« Perches » ne montre que les événements où l'hôte va.

**L'hôte** ouvre sa liste avec un titre et son e-mail, et arrive sur la page d'édition :
l'introduction (Markdown simple : paragraphes, gras, italique, listes, liens), puis les
événements. Il n'y a ni compte ni mot de passe : l'édition est un lien secret, que son
navigateur retient et que son e-mail lui renvoie. Il partage sa page, ou un seul
événement, là où ses amis sont déjà (WhatsApp, Signal, e-mail) ; le lien s'affiche avec
une image d'aperçu. Rien ne le notifie ; il lit les réponses quand il veut. Il peut
modifier, annuler et rétablir une perche, effacer une réponse, fermer sa liste ; chaque
geste irréversible se confirme.

**L'invité** ouvre le lien, lit, et répond à une perche avec un prénom : « j'y serai »,
« peut-être » ou « j'aurais bien aimé ». Il peut laisser une ligne, que l'hôte lit sans
y répondre, et un e-mail s'il veut le rappel de la veille, les changements de date ou
de lieu et un fichier calendrier.

**Après**, la perche passée quitte la page. Sa propre page reste lisible trente jours,
prénoms compris, puis ne montre plus que « c'est passé » ; l'e-mail d'un invité est
effacé à ce moment. L'hôte garde tout dans son export.

## Les conventions

Elles tiennent dans la structure des pages (les champs présents et absents) et dans la
suite de tests.

1. Ne pas répondre est aussi une réponse : le silence vaut « pas cette fois ».
2. Il n'y a pas de « non ». Trois réponses, aucune n'est un refus.
3. Pas de fil de discussion : au plus une ligne, lue, sans réponse attendue.
4. *Retirée le 28 août 2026* : une perche ne compte pas les places.
5. L'hôte y va de toute façon ; il n'y a pas d'option « à confirmer ».
6. L'introduction de l'hôte ouvre sa page et accompagne chaque perche ; pas de champ
   « état » à part.
7. Rien ne notifie l'hôte.
8. L'invité n'a jamais de compte.
9. Ce qui est passé quitte la page ; l'hôte garde son export.
10. Le texte libre passe avant les champs.
11. Les invités ne sont prévenus que de ce qui ferait manquer le rendez-vous : annulation,
    changement de date ou de lieu.
12. Un repéré ne demande rien. La perche est un geste posé dessus, avec les dates de
    l'hôte ; elle se retire sans que l'événement quitte la page.

## Hors périmètre

Fil de discussion, bouton « non », découverte, abonnés, notifications poussées,
visibilité entre listes (les amis des amis ne voient rien), monétisation, plugins.
Voir [CONTRIBUTING.md](CONTRIBUTING.md).

## Architecture

Go (bibliothèque standard, plus `golang.org/x/image` pour les images d'aperçu), SQLite,
rendu côté serveur, un conteneur. Le seul JavaScript est un bouton « copier » et
l'aperçu de l'adresse à la création, tous deux facultatifs. Les deux polices sont
embarquées ; une page ne charge rien depuis un autre site, et l'image d'un événement
est copiée sur le serveur plutôt que liée.

N'importe qui peut faire tourner une instance ; une liste s'exporte à tout moment (ICS,
JSON public, export complet pour l'hôte). Une instance accepte les nouvelles listes
librement, sur invitation (un hôte invite depuis son édition) ou pas du tout.

Garde-fous : corps et champs bornés, limite de débit par adresse IP (en mémoire, rien
n'est conservé), pot de miel sur le formulaire de réponse, en-têtes de sécurité et CSP,
`noindex` sur toutes les pages, e-mails des invités effacés un mois après la date.

Les choix sont notés dans [DECISIONS.md](DECISIONS.md), une ligne datée par choix. Les
textes suivent [docs/ecriture.md](docs/ecriture.md).

## Installation

```sh
docker compose up -d        # l'instance écoute sur :8080
```

Ou sans Docker : `go build && ./perches`.

Variables d'environnement : `PERCHES_DB` (chemin SQLite), `PERCHES_ADDR`,
`PERCHES_BASE_URL`, `PERCHES_POLITIQUE` (`ouverte` | `invitation` | `fermee`),
`PERCHES_DERRIERE_PROXY` (croire X-Forwarded-For, seulement derrière un reverse proxy),
`PERCHES_SMTP_HOTE/PORT/UTILISATEUR/MDP/DE` (sans SMTP, les courriels sont
journalisés). Un lien d'invitation (`/?code=…`, à envoyer à la personne) se génère avec
`perches -nouveau-code`.

Toutes les heures, l'instance écrit une copie cohérente de sa base à côté d'elle
(`perches.sauvegarde.db`, par `VACUUM INTO`) ; la sauvegarde de l'hôte n'a qu'à
emporter le dossier de données.

Les conventions sont encodées dans la suite de tests : `go test ./...`
(voir [docs/tests-conventions.md](docs/tests-conventions.md)).

## Licence

[AGPL-3.0](LICENSE).
