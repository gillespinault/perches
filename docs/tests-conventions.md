# Tests de conventions

Chaque convention du [README](../README.md) est traduite ici en assertions
falsifiables, formulées au niveau HTTP/HTML pour se traduire directement en tests
Go (`httptest`) pendant le week-end v0. **Cette suite est un outil de gouvernance
autant que de non-régression** : une PR qui modifie un de ces tests est une PR qui
demande à changer le produit.

Deux niveaux :

- **[S]** structure — la contrainte vit dans le schéma SQLite, testée par insertion directe ;
- **[H]** HTTP/HTML — la contrainte vit dans les pages et les formulaires.

Convention de nommage Go : `TestConvNN_NomParlant`.

---

## C1 — Le silence vaut « pas cette fois »

- `TestConv01_LeSystemeIgnoreLesNonRepondants` [S] — le modèle ne connaît que les
  réponses : aucune table ni colonne ne représente un invité attendu, relancé ou en
  attente. (Test par introspection du schéma : les tables sont exactement celles de
  `schema.sql`.)
- `TestConv01_AucuneRelance` [H] — le mailer factice ne reçoit jamais d'envoi vers
  une adresse qui n'a pas répondu ; les types d'envoi possibles sont bornés par la
  contrainte CHECK de la table `envois`.

## C2 — Aucun signal négatif

- `TestConv02_AucunBoutonNon` [H] — le HTML d'une intention ne contient aucun
  contrôle de refus : les seules valeurs de formulaire pour `statut` sont
  `jy_serai`, `peut_etre` et `jaurais_aime` (« ah zut, j'aurais bien aimé » — l'envie,
  pas l'absence ; décision du 28 août 2026).
- `TestConv02_PostNonRejete` [H] — `POST` avec `statut=non` → 400.
- `TestConv02_SchemaRefuseLeNon` [S] — `INSERT … statut='non'` → violation de
  CHECK (vérifié le 2026-08-27 sur le schéma).
- `TestConv02_CapacitePleineNeBloquePas` [H] — capacité 2, trois réponses : le
  formulaire reste actif, le POST de la 4e réponse est accepté, et ni « complet »
  ni aucun équivalent n'apparaît dans le HTML.

## C3 — Pas de fil de discussion

- `TestConv03_MotUneLigneMax` [H] — le mot est refusé s'il contient un saut de
  ligne ou dépasse la longueur max ; un seul mot par réponse, pas d'édition en
  fil.
- `TestConv03_MotsInvisiblesDesAutresInvites` [H] — le HTML public d'une intention
  ne contient le mot d'aucune réponse ; seuls l'hôte (via jeton d'édition) les lit.
- `TestConv03_AucunBoutonRepondre` [H] — la vue hôte des mots n'offre aucun
  mécanisme de réponse.

## C4 — Capacité indicative, jamais bloquante

- `TestConv04_CapaciteAffichee` [H] — si une capacité est définie, la page la
  montre (« Capacité indicative : N » seule, ou « … — 2 sur 4 places environ » avec les
  présences).
- Le versant « jamais bloquante » est couvert par
  `TestConv02_CapacitePleineNeBloquePas`.

## C5 — « J'y vais de toute façon » par défaut

- `TestConv05_DefautSchemaEtAffichage` [S+H] — une intention créée sans préciser a
  `jy_vais_de_toute_facon = 1`, et la page l'affiche.

## C6 — La voix de l'hôte est visible (ex-« état » ; décision du 28 août 2026)

- `TestConv06_LaVoixDeLHoteSurChaquePage` [H] — l'introduction apparaît sur la page de la
  liste et sur chaque perche ; aucun champ « état » n'est affiché
  publique et sur chaque page d'intention.

## C7 — Aucune notification poussée vers l'hôte

- `TestConv07_ReponseSansEnvoi` [H] — une nouvelle réponse ne déclenche aucun
  envoi : le mailer factice reste vide. Le résumé hebdomadaire n'existe que si
  l'hôte l'a demandé (opt-in).

## C8 — L'invité n'a jamais de compte

- `TestConv08_ReponseSansCookieNiSession` [H] — répondre ne requiert que prénom et
  statut ; aucun cookie, aucune session, aucune vérification d'e-mail. Pas de
  table `users` dans le schéma (couvert par `TestConv01_LeSystemeIgnoreLesNonRepondants`).

## C9 — Ce qui est passé disparaît de la page ; l'hôte garde ses lettres

- `TestConv09_IntentionPasseeAbsenteDuPublic` [H] — une intention dont `quand` est
  passé n'apparaît plus sur la page publique de la liste.
- `TestConv09_ArchiveViaJetonEdition` [H] — la même intention, avec ses réponses
  complètes (prénoms et mots), reste lisible via le lien secret d'édition.
- `TestConv09_ReponsesPubliquesEffaceesApresDelai` [H] — après le délai, aucun
  prénom d'une intention passée n'apparaît sur aucune page publique — mais les
  lignes existent toujours en base.

## C10 — La page est une lettre avant d'être un formulaire

- `TestConv10_LettreAvantMecanique` [H] — dans le HTML de la page publique, la
  lettre précède la première intention ; sur une page d'intention, la description
  précède le formulaire.

## C11 — Logistique oui, social jamais

- `TestConv11_AnnulationNotifiee` [H] — annuler (ou changer date/lieu) déclenche
  exactement un envoi `logistique` par réponse ayant un e-mail, zéro pour les
  autres.
- `TestConv11_RienDeSocialNEstEnvoye` [H] — nouvelle réponse, nouveau mot,
  nouveau lien de liste : zéro envoi, à quiconque.
- `TestConv11_TypesDEnvoiBornes` [S] — `INSERT … type='nouveau_participant'` dans
  `envois` → violation de CHECK.

---

## Décisions annexes (DECISIONS.md), également testées

- `TestDecision_CarteOGToujoursComplete` [H] — les balises `og:title`,
  `og:description` (date, lieu) sont présentes même pour une intention
  `visibilite='lien'`.
- `TestDecision_LienSeulementAbsentDeLaListe` [H] — une intention
  `visibilite='lien'` n'apparaît pas sur la page publique de la liste, mais
  répond en 200 sur `/i/{jeton}`.
- `TestDecision_HoteEffaceUneReponse` [H] — suppression d'une réponse via le
  jeton d'édition ; refusée sans lui (404, pas 403 — le jeton inconnu ne révèle
  rien).
- `TestDecision_SansEmailLaPageLeDit` [H] — la confirmation d'une réponse sans
  e-mail contient la mention « revérifie la page avant d'y aller ».
- `TestDecision_ExportsToujoursDisponibles` [H] — `/l/{slug}.ics`,
  `/l/{slug}.json` et l'export complet de l'atelier répondent 200 à tout moment.
- `TestDecision_IntentionOuverteExigeEcheance` [S] — `quand` NULL sans
  `echeance_decision` → violation de CHECK (vérifié le 2026-08-27).
- `TestDecision_CodeInvitationRequis` [H] — instance en mode code d'invitation :
  création de liste sans code valide → refus.

## Hors périmètre : tests d'absence

Un petit test d'introspection, `TestHorsPerimetre_LeSchemaResteMaigre`, verrouille
la liste exacte des tables. Ajouter une table (`commentaires`, `abonnements`,
`notifications`, `users`…) casse ce test : la discussion sur le périmètre a lieu
face à un test rouge, pas dans un fil d'issue.

## Après la revue du 28 août 2026

Les tests `TestSecurite_*` (lot robustesse) et `TestDecision_*` (décisions produit et
revue UX) ne correspondent pas à une convention du README mais à une ligne de
DECISIONS.md ; leur nom dit la décision. `go test -v` en est la table des matières.
