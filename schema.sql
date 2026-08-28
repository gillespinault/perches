-- Perches — schéma SQLite.
--
-- Ce schéma se lit aussi par ses absences : pas de table users, pas de
-- commentaires, pas de notifications, pas d'abonnés, pas d'accusés de
-- lecture. Les conventions vivent dans la structure (voir
-- docs/tests-conventions.md).
--
-- Vocabulaire du projet, en français : liste, intention, réponse, perche
-- (le lien partagé), croisement.

PRAGMA foreign_keys = ON;

-- Une liste par personne. La page est une lettre avant d'être un formulaire.
CREATE TABLE listes (
    id            INTEGER PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,   -- lien public : /l/{slug}
    jeton_edition TEXT NOT NULL UNIQUE,   -- lien secret d'édition (seule identité de l'hôte)
    titre         TEXT NOT NULL,
    lettre        TEXT NOT NULL DEFAULT '', -- texte libre, affiché avant la mécanique
    etat          TEXT NOT NULL DEFAULT '', -- « je rouvre doucement », « en retrait », …
    email         TEXT,                    -- retrouver l'édition (demandé à l'ouverture)
    fermee_le     TEXT,                    -- liste fermée : la page ne montre plus rien
    cree_le       TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE intentions (
    id           INTEGER PRIMARY KEY,
    liste_id     INTEGER NOT NULL REFERENCES listes(id) ON DELETE CASCADE,
    jeton        TEXT NOT NULL UNIQUE,     -- la perche : lien direct /i/{jeton}
    titre        TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    quand        TEXT,                     -- ISO 8601 ; NULL = « à fixer » (intention ouverte)
    fin          TEXT,                     -- dernier jour (date ISO) si la perche dure plusieurs jours
    echeance_decision TEXT,                -- intention ouverte : sans échéance, elle recréerait l'attente
    lieu         TEXT NOT NULL DEFAULT '',
    url_externe  TEXT,                     -- l'expo, le festival — carte d'aperçu et coïncidences
    jy_vais_de_toute_facon INTEGER NOT NULL DEFAULT 1,
    -- Tout est repéré (l'événement : ses dates, son lieu). La perche est un geste posé dessus :
    -- « j'y vais, si ça te dit » — avec ses propres dates, celles de l'hôte (lot F, 2026-08-28).
    perche_tendue_le TEXT,                 -- NULL = repéré, sans engagement ni réponse
    perche_quand TEXT,                     -- quand j'y vais (ISO 8601) ; NULL = les dates de l'événement
    perche_fin   TEXT,                     -- dernier jour où j'y suis, si ça dure
    visibilite   TEXT NOT NULL DEFAULT 'page' CHECK (visibilite IN ('page', 'lien')),
    annulee_le   TEXT,                     -- annulation = logistique : notifiée aux e-mails connus
    cree_le      TEXT NOT NULL DEFAULT (datetime('now')),
    -- une intention est bornée : datée, ou ouverte avec échéance de décision
    CHECK (quand IS NOT NULL OR echeance_decision IS NOT NULL),
    -- les dates de la perche n'existent que si la perche est tendue
    CHECK (perche_quand IS NULL OR perche_tendue_le IS NOT NULL)
);

-- Trois réponses, aucune n'est un refus : « j'aurais bien aimé » dit l'envie, pas l'absence.
-- Le « non » n'existe pas, par contrainte.
CREATE TABLE reponses (
    id             INTEGER PRIMARY KEY,
    intention_id   INTEGER NOT NULL REFERENCES intentions(id) ON DELETE CASCADE,
    prenom         TEXT NOT NULL,
    statut         TEXT NOT NULL CHECK (statut IN ('jy_serai', 'peut_etre', 'jaurais_aime')),
    mot            TEXT NOT NULL DEFAULT '', -- une ligne, lue par l'hôte, sans réponse attendue
    prenom_visible INTEGER NOT NULL DEFAULT 1,
    email          TEXT,                     -- rappel la veille + logistique, optionnel
    cree_le        TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Variante ouverte : jours possibles indiqués par une réponse, sans engagement.
CREATE TABLE disponibilites (
    reponse_id INTEGER NOT NULL REFERENCES reponses(id) ON DELETE CASCADE,
    jour       TEXT NOT NULL, -- date ISO
    PRIMARY KEY (reponse_id, jour)
);

-- Croisements (phase 2) : le lien entre deux listes est une donnée — symétrique,
-- choisi des deux côtés (proposé par A, effectif quand B confirme). La
-- coïncidence, elle, est un calcul, pas une table. Pas de transitivité.
CREATE TABLE liens_listes (
    liste_a    INTEGER NOT NULL REFERENCES listes(id) ON DELETE CASCADE,
    liste_b    INTEGER NOT NULL REFERENCES listes(id) ON DELETE CASCADE,
    confirme_le TEXT,  -- NULL = proposé, pas encore accepté ; le croisement n'existe qu'après
    cree_le    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (liste_a, liste_b),
    CHECK (liste_a <> liste_b)
);

-- Politique d'instance « code d'invitation » (réglage de départ de l'instance de Gilles).
CREATE TABLE codes_invitation (
    code       TEXT PRIMARY KEY,
    cree_le    TEXT NOT NULL DEFAULT (datetime('now')),
    utilise_le TEXT,
    liste_id   INTEGER REFERENCES listes(id) ON DELETE SET NULL
);

-- Journal des e-mails partis (idempotence des rappels et des avis logistiques —
-- jamais utilisé pour du social).
CREATE TABLE envois (
    id         INTEGER PRIMARY KEY,
    reponse_id INTEGER REFERENCES reponses(id) ON DELETE CASCADE,
    liste_id   INTEGER REFERENCES listes(id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('rappel_veille', 'logistique', 'recuperation_lien', 'resume_hebdo')),
    envoye_le  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_intentions_liste ON intentions(liste_id, quand);
CREATE INDEX idx_reponses_intention ON reponses(intention_id);
