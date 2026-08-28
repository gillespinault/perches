# Perches

> « Pour info, j'ai l'intention de faire telle activité, à tel moment.
> Si quelqu'un veut se joindre à moi, ça me fait plaisir. »

Perches permet de dire cela à peu de frais et sans créer d'obligation — et à ses amis
de répondre d'un geste : sans compte, sans application, sans négociation, sans que le
silence soit un refus.

**Statut : v0.** Le geste de base fonctionne : liste, intentions datées, réponses,
exports, rappels. La variante « à fixer » et les croisements viendront — ou pas — plus tard.
Ce projet est maintenu par intermittence ; le silence vaut « pas maintenant ».

## Le geste : l'intention, pas l'invitation

Une intention comporte une activité, un moment (ou « à fixer »), un lieu, une capacité
indicative, et la mention « j'y vais de toute façon ». Ce qui la distingue d'une
invitation :

- **Elle ne demande rien.** Aucune réponse n'est attendue ; le silence vaut « pas cette fois ».
- **Elle n'a pas de négatif.** On peut dire « j'y serai » ou « peut-être ». Il n'existe pas de bouton « non » — ni pour l'invité (aucun refus à formuler), ni pour l'hôte (aucun refus à lire).
- **Elle est bornée.** Une date, une durée implicite. Elle se termine d'elle-même.
- **Elle est asymétrique et l'assume.** L'hôte y va de toute façon ; l'invité s'ajoute ou non. Personne ne dépend de personne.
- **Elle porte son mode d'emploi.** Les conventions sont affichées par l'outil, pas par la personne.

## Les conventions

Elles sont le produit ; tout le reste est de la plomberie. Elles vivent dans la
structure — boutons, champs, absence de champs — et sont encodées en tests.

1. Le silence vaut « pas cette fois ».
2. Aucun signal négatif n'existe dans le système. Trois réponses — « j'y serai », « peut-être », « ah zut, j'aurais bien aimé » — et aucune n'est un refus.
3. Pas de fil de discussion. Au plus une ligne, lue, sans réponse attendue.
4. Capacité affichée sur chaque intention — indicative, jamais bloquante.
5. « J'y vais de toute façon » par défaut.
6. L'état de l'hôte est visible.
7. Aucune notification poussée vers l'hôte ; il va chercher l'information quand il veut.
8. L'invité n'a jamais de compte.
9. Ce qui est passé disparaît de la page ; l'hôte garde ses lettres.
10. La page est une lettre avant d'être un formulaire : le texte libre passe avant la mécanique.
11. On ne notifie que ce qui ferait manquer le rendez-vous (annulation, changement de date ou de lieu) — jamais rien de social.

## Comment ça marche

**L'hôte** crée sa liste — une page dans sa voix : une lettre en texte libre, un état
(« je rouvre doucement », « en retrait pour l'instant », « disponible »), des
intentions. Il reçoit un lien public et un lien secret d'édition — pas de compte, pas
de mot de passe. Il partage la liste ou une seule intention là où ses amis sont déjà
(WhatsApp, Signal, e-mail, Mastodon…) ; le lien s'affiche sous forme de carte. Il
consulte un résumé quand il veut ; rien ne le notifie.

**L'invité** ouvre le lien, lit, tape « j'y serai » ou « peut-être », donne un prénom.
Il peut laisser une ligne, lue par l'hôte, sans réponse attendue — l'outil le dit.
Optionnel : un e-mail pour le rappel de la veille et les changements logistiques, et
un fichier calendrier. Rien d'autre.

**Après** : l'activité a lieu, avec ou sans compagnie. L'intention passée disparaît de
la page ; l'hôte garde ses lettres.

## Hors périmètre, par principe

Fil de discussion, bouton « non », découverte, abonnés, notifications poussées,
transitivité (les amis des amis ne voient rien), monétisation, système de plugins.
Une contribution qui contredit une convention est refusée gentiment, par principe —
voir [CONTRIBUTING.md](CONTRIBUTING.md).

## Architecture

Décentralisé comme le courriel : n'importe qui peut faire tourner une instance, chaque
liste est exportable à tout moment (HTML statique, ICS, JSON), les invités n'ont
besoin que d'un navigateur. Une instance peut s'éteindre sans rien faire perdre à
personne.

Une instance = un petit serveur : **Go, rendu côté serveur, SQLite, un conteneur,
presque pas de JavaScript**. Identité de l'hôte = lien secret d'édition, avec e-mail
optionnel de récupération. Politique d'instance au choix : création de listes ouverte,
sur code d'invitation, ou fermée.

Les choix d'architecture sont notés dans [DECISIONS.md](DECISIONS.md), une ligne datée
par choix.

## Installation

```sh
docker compose up -d        # l'instance écoute sur :8080
```

Ou sans Docker : `go build && ./perches`.

Variables d'environnement : `PERCHES_DB` (chemin SQLite), `PERCHES_ADDR`,
`PERCHES_BASE_URL`, `PERCHES_POLITIQUE` (`ouverte` | `invitation` | `fermee`),
`PERCHES_SMTP_HOTE/PORT/UTILISATEUR/MDP/DE` (sans SMTP, les courriels sont
journalisés). Un lien d'invitation (`/?code=…`, à envoyer à la personne) se génère avec `perches -nouveau-code`.

Les conventions sont encodées dans la suite de tests : `go test ./...`
(voir [docs/tests-conventions.md](docs/tests-conventions.md)).

## Licence

[AGPL-3.0](LICENSE).
