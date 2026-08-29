# Contribuer

Le projet est maintenu par intermittence. Les issues sont lues ; une réponse peut
prendre du temps, et une absence de réponse veut dire « pas maintenant ». Il n'y a pas
de salon de discussion.

## Périmètre

Perches fait une chose : des listes d'événements où l'hôte dit s'il y va, et où ses
amis peuvent dire qu'ils viennent. Ce qui n'y entrera pas :

- fil de discussion ;
- bouton « non », ou tout autre signal négatif ;
- découverte, abonnés, fil d'actualité ;
- notification vers l'hôte ;
- notification sociale vers l'invité (seule la logistique est envoyée : annulation,
  changement de date ou de lieu) ;
- visibilité entre listes (les amis des amis ne voient rien) ;
- comptes pour les invités ;
- monétisation ;
- plugins.

Une contribution qui va dans un de ces sens sera refusée, quelle que soit sa qualité ;
c'est ce qui garde le projet petit.

## Les tests

Chaque convention du [README](README.md) a son test. Une PR qui modifie un test de
convention demande à changer le produit, et sera lue comme telle.

## Pile et textes

Go, SQLite, rendu côté serveur, presque pas de JavaScript, deux dépendances (le pilote
SQLite et `golang.org/x/image`). Versions taguées quand c'est prêt, sans calendrier.
Les textes, dans le code comme dans le dépôt, suivent [docs/ecriture.md](docs/ecriture.md).
