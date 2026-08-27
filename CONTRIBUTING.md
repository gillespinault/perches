# Contribuer à Perches

## Mode de maintenance

Ce projet est maintenu par intermittence, par choix — il suit ses propres conventions.
Le silence vaut « pas maintenant », jamais « non ». Les réponses lentes sont assumées.
Pas de salon de discussion : les issues suffisent.

## Périmètre

Perches fait une chose : des listes d'intentions avec porte ouverte. Une page par
personne, des intentions bornées, un geste pour s'y joindre, des conventions portées
par la structure.

Hors périmètre, par principe :

- fil de discussion ;
- bouton « non », ou tout signal négatif ;
- découverte, abonnés, fil d'actualité ;
- notification poussée vers l'hôte ;
- notification sociale vers l'invité — seule la logistique (annulation, changement de
  date ou de lieu) est notifiée ;
- transitivité entre listes liées (les amis des amis ne voient rien) ;
- comptes pour les invités ;
- monétisation ;
- système de plugins.

Une contribution qui contredit une convention est refusée gentiment, **par principe**.
Ce n'est pas un jugement sur sa qualité : c'est ce qui rend la maintenance légère et
le produit cohérent. Dire non est prévu.

## Les conventions sont des tests

Chaque convention du [README](README.md) est encodée dans la suite de tests. Personne
ne peut ajouter un bouton « non » sans la casser — le débat est réglé par la CI, pas
par la discussion. Une PR qui modifie un test de convention est une PR qui demande à
changer le produit : elle sera lue comme telle.

## Pile

Go, SQLite, rendu côté serveur, presque pas de JavaScript, dépendances minimales. La
pile est ennuyeuse par choix. Versions taguées quand c'est prêt, sans calendrier.
