# Perches

> « Pour info, j'ai l'intention de faire telle activité, à tel moment.
> Si quelqu'un veut se joindre à moi, ça me fait plaisir. »

Perches permet de dire cela à peu de frais et sans créer d'obligation — et à ses amis
de répondre d'un geste : sans compte, sans application, sans négociation, sans que le
silence soit un refus.

**Statut : v0, en service** (perches.robotsinlove.be, août 2026). Le geste de base
fonctionne : liste, perches datées (un jour ou plusieurs), trois réponses, rappels,
courriels logistiques, exports ICS/JSON, édition avec confirmations et archive. Relu par
une revue croisée (hôte, invité, sécurité, produit) le 28 août 2026, puis repris dans une
seconde passe le même jour : typographie, mise en page, ton de l'interface.
La variante « à fixer » et les croisements viendront — ou pas — plus tard.
Ce projet est maintenu par intermittence ; le silence vaut « pas maintenant ».

## Le geste : l'intention, pas l'invitation

Une intention — une *perche*, à l'écran — comporte une activité, un moment (« à fixer »
viendra plus tard), éventuellement un dernier jour si elle dure, un lieu, et la mention
« j'y vais de toute façon ». Ce qui la distingue d'une invitation :

- **Elle ne demande rien.** Aucune réponse n'est attendue ; le silence vaut « pas cette fois ».
- **Elle n'a pas de négatif.** On peut dire « j'y serai », « peut-être » ou « j'aurais bien aimé » — cette dernière dit l'envie, pas l'absence. Il n'existe pas de bouton « non » — ni pour l'invité (aucun refus à formuler), ni pour l'hôte (aucun refus à lire).
- **Elle est bornée.** Une date — ou un premier et un dernier jour. Elle se termine d'elle-même.
- **Elle est asymétrique et l'assume.** L'hôte y va de toute façon ; l'invité s'ajoute ou non. Personne ne dépend de personne.
- **Elle porte son mode d'emploi.** Les conventions sont affichées par l'outil, pas par la personne — en une phrase, sans commentaire.

## Tout est repéré ; la perche est un geste

Depuis le 28 août 2026, une liste est une chronologie d'événements *repérés* — « ça m'a
l'air intéressant » : un titre, les dates de l'événement, un lieu, un lien, quelques mots.
Signalé, sans engagement, sans formulaire de réponse. Sur certains, l'hôte *tend une
perche* : « j'y vais, si ça te dit » — avec **ses propres dates** (le samedi d'un festival
de cinq jours), qui sont les seules que les invités reçoivent : dans la chronologie, le
rappel de la veille, le fichier calendrier. Une liste qui propose est plus généreuse
qu'une liste qui demande, et elle vit entre deux perches. La perche se tend et se retire
d'un geste ; l'événement reste. Un filtre « Perches » sur la page ne garde que les
événements où l'hôte y va.

## Les conventions

Elles sont le produit ; tout le reste est de la plomberie. Elles vivent dans la
structure — boutons, champs, absence de champs — et sont encodées en tests.

1. Le silence vaut « pas cette fois ».
2. Aucun signal négatif n'existe dans le système. Trois réponses — « j'y serai », « peut-être », « j'aurais bien aimé » — et aucune n'est un refus.
3. Pas de fil de discussion. Au plus une ligne, lue, sans réponse attendue.
4. *Retirée le 28 août 2026.* Une perche ne compte pas les places : un nombre serait déjà une manière de dire non.
5. « J'y vais de toute façon » — toujours, sans option.
6. La voix de l'hôte est visible partout : son introduction ouvre sa page et accompagne chaque perche. Pas de champ « état » à part — il dit où il en est dans ses mots.
7. Aucune notification poussée vers l'hôte ; il va chercher l'information quand il veut.
8. L'invité n'a jamais de compte.
9. Ce qui est passé disparaît de la page ; l'hôte garde ses lettres.
10. La page est une lettre avant d'être un formulaire : le texte libre passe avant la mécanique.
11. On ne notifie que ce qui ferait manquer le rendez-vous (annulation, changement de date ou de lieu) — jamais rien de social.
12. Tout est *repéré* et ne demande rien : ni réponse, ni engagement. La perche est un geste posé dessus, avec les dates de l'hôte ; elle se tend et se retire, l'événement reste.

## Comment ça marche

**L'hôte** ouvre sa liste (un titre, son e-mail) et arrive sur son *édition* — une page
dans sa voix : une introduction en Markdown de lettre (paragraphes, gras, italique,
listes, liens), puis des perches. L'édition est identifiée par un lien secret — pas de
compte, pas de mot de passe ; son navigateur s'en souvient, son e-mail le lui renvoie.
Il partage sa page ou une seule perche là où ses amis sont déjà (WhatsApp, Signal,
e-mail…) ; le lien s'affiche sous forme de carte. Il consulte son édition quand il veut ;
rien ne le notifie. Un menu range les gestes rares : inviter, réglages, export, thème. Il peut modifier une perche, l'annuler (et la rétablir), effacer une
réponse — chaque geste irréversible se confirme — et fermer sa liste.

**L'invité** ouvre le lien, lit, ouvre une perche sur place, choisit « j'y serai », « peut-être » ou « j'aurais bien aimé », donne un prénom.
Il peut laisser une ligne, lue par l'hôte, sans réponse attendue.
Optionnel : un e-mail pour le rappel de la veille et les changements logistiques, et
un fichier calendrier. Rien d'autre.

**Après** : l'activité a lieu, avec ou sans compagnie. La perche passée disparaît de
la page ; sa propre page reste lisible (prénoms compris) trente jours, puis ne montre
plus que « c'est passé » ; l'e-mail d'un invité est effacé au même moment. L'hôte
garde tout dans son archive.

## Hors périmètre, par principe

Fil de discussion, bouton « non », découverte, abonnés, notifications poussées,
transitivité (les amis des amis ne voient rien), monétisation, système de plugins.
Une contribution qui contredit une convention est refusée gentiment, par principe —
voir [CONTRIBUTING.md](CONTRIBUTING.md).

## Architecture

Décentralisé comme le courriel : n'importe qui peut faire tourner une instance, chaque
liste est exportable à tout moment (ICS, JSON public, export complet de l'hôte), les invités n'ont
besoin que d'un navigateur. Une instance peut s'éteindre sans rien faire perdre à
personne.

Une instance = un petit serveur : **Go, rendu côté serveur, SQLite, un conteneur,
presque pas de JavaScript** ; les cartes de partage (l'image sous un lien dans WhatsApp,
Signal, Messages…) sont dessinées par le serveur, une par liste et par événement (un bouton « copier » et l'aperçu de l'adresse, en
amélioration progressive). Identité de l'hôte = lien secret d'édition, que son e-mail
(demandé à l'ouverture) permet de retrouver ; cookie côté hôte seulement. Politique
d'instance au choix : création de listes ouverte, sur invitation (un hôte peut inviter
depuis son édition), ou fermée. Deux polices auto-hébergées (Newsreader pour ce que
l'hôte écrit, Instrument Sans pour ce que l'outil affiche), aucune requête externe. Garde-fous : corps et champs bornés, limite de débit,
pot de miel, en-têtes de sécurité et CSP, e-mails des invités purgés après un mois.

Les choix d'architecture sont notés dans [DECISIONS.md](DECISIONS.md), une ligne datée
par choix.

## Installation

```sh
docker compose up -d        # l'instance écoute sur :8080
```

Ou sans Docker : `go build && ./perches`.

Variables d'environnement : `PERCHES_DB` (chemin SQLite), `PERCHES_ADDR`,
`PERCHES_BASE_URL`, `PERCHES_POLITIQUE` (`ouverte` | `invitation` | `fermee`),
`PERCHES_DERRIERE_PROXY` (croire X-Forwarded-For — seulement derrière un reverse proxy),
`PERCHES_SMTP_HOTE/PORT/UTILISATEUR/MDP/DE` (sans SMTP, les courriels sont
journalisés). Un lien d'invitation (`/?code=…`, à envoyer à la personne) se génère avec `perches -nouveau-code`.

Toutes les heures, l'instance écrit une copie cohérente de sa base à côté d'elle
(`perches.sauvegarde.db`, par `VACUUM INTO`) : la sauvegarde de l'hôte n'a qu'à emporter
le dossier de données, sans se soucier d'une écriture en cours.

Les conventions sont encodées dans la suite de tests : `go test ./...`
(voir [docs/tests-conventions.md](docs/tests-conventions.md)).

## Licence

[AGPL-3.0](LICENSE).
