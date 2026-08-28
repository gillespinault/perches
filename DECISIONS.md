# DECISIONS

Une ligne datée par choix. Contexte complet : document projet dans le wiki.

- 2026-08-27 — Nom : **Perches**. Vocabulaire : liste, intention, perche (le lien partagé), hôte, invité, état, croisement.
- 2026-08-27 — Construction neuve, pas un fork de Gathio ; on reprend ses choix éprouvés (lien secret d'édition, accès par lien, effacement automatique, pas d'OAuth).
- 2026-08-27 — Langage : **Go** — binaire unique, rendu côté serveur, SQLite, un conteneur, presque pas de JavaScript.
- 2026-08-27 — Licence : **AGPL-3.0**.
- 2026-08-27 — Pas d'étape Gathio : le premier usage est un statement, l'outil fait partie du message. v0 bornée à un week-end.
- 2026-08-27 — Capacité indicative, jamais bloquante : aucun bouton ne se grise, les invités se régulent d'eux-mêmes.
- 2026-08-27 — Notifications : logistique seulement (annulation, date, lieu), jamais social. Canal unique = l'e-mail optionnel du rappel ; sans e-mail, l'invité revérifie la page.
- 2026-08-27 — Carte Open Graph toujours complète, y compris pour les intentions « lien seulement » : une perche tendue est visible de quiconque tient le lien.
- 2026-08-27 — L'effacement concerne la page publique ; l'hôte garde une archive privée complète (prénoms et mots compris, comme des lettres reçues).
- 2026-08-27 — Lien secret perdu sans e-mail de récupération : liste orpheline, assumé — elle se vide d'elle-même à mesure que ses intentions passent.
- 2026-08-27 — Une liste par personne ; la visibilité « lien seulement » par intention remplace les listes multiples.
- 2026-08-27 — L'hôte peut effacer une réponse ; rate limiting minimal sur les formulaires sans compte.
- 2026-08-27 — Instance de Gilles : création de listes sur code d'invitation.
- 2026-08-27 — Vocabulaire du domaine en français jusque dans le schéma SQL (listes, intentions, reponses) : le vocabulaire fait partie du produit.
- 2026-08-27 — Contraintes CHECK comme première ligne des conventions : le statut « non » et l'intention sans borne (ni date ni échéance) sont impossibles en base, pas seulement dans l'interface.
- 2026-08-27 — v0 : stdlib `net/http` (routes Go 1.22+), `modernc.org/sqlite` (pur Go, sans cgo), templates et schéma embarqués dans le binaire, zéro JavaScript, zéro cookie.
- 2026-08-27 — Identité de l'invité : rien — pas même un cookie. Répondre est un POST anonyme portant un prénom.
- 2026-08-27 — Rappels de la veille : boucle horaire in-process, idempotente via le journal `envois`. Pas de cron externe.
- 2026-08-27 — Effacement public des réponses : 30 jours après la date de l'intention. La v0 exige une date (« à fixer » viendra avec la variante ouverte).
- 2026-08-28 — L'invitation n'existe pas dans l'interface : un lien `/?code=…` sorti de la CLI ouvre le formulaire, et l'accueil sans code ne parle ni de création ni d'invitation. Un ami qui tombe sur l'accueil n'a rien à y faire.
- 2026-08-28 — Ouvrir une liste = trois champs (titre, adresse, e-mail) puis l'atelier directement : la lettre s'écrit là où elle se relit. Pas de page intermédiaire ; la clé d'édition est l'adresse de l'atelier.
- 2026-08-28 — Partager = copier un lien, rien d'autre. Chaque page et chaque perche a son lien copiable dans l'atelier. Une seule touche de JavaScript, en amélioration progressive : le bouton « copier ».
- 2026-08-28 — Vocabulaire à l'écran : « atelier » pour la page d'édition, « clé » pour le lien secret, « perche tendue » pour une intention à venir. Le formulaire d'une perche montre quatre champs ; le reste sous « Plus d'options ».
- 2026-08-28 — Le « silence vaut pas cette fois » reste une convention du système, pas un slogan : une mention douce sur la page de réponse, rien en pied de page ni à l'accueil.
- 2026-08-28 — « État » reste le mot du domaine (schéma, README) ; à l'écran il devient « En ce moment », pour ne pas sonner comme un diagnostic.
- 2026-08-28 — Troisième réponse « ah zut, j'aurais bien aimé », à part entière et visible comme les autres : elle dit l'envie, pas l'absence. Le « non » reste impossible. Pas de rappel de la veille pour qui ne vient pas.
- 2026-08-28 — Anti-nuisance sans friction : pot de miel sur le formulaire de réponse, `noindex` sur toutes les pages (on n'y arrive que par un lien), limite de débit par IP. Rien qui demande quoi que ce soit à un humain.
- 2026-08-28 — Pas de login. Le navigateur de l'hôte retient son atelier (cookie HttpOnly, un an, posé uniquement en visitant `/e/…`) : taper l'adresse du site ramène à l'atelier. « Zéro cookie » reste vrai pour l'invité. Le lien secret par e-mail reste la clé de secours (SMTP Gmail de l'instance).
