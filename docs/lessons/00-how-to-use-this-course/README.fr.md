# Leçon 00 — Comment utiliser ce cours

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 0 (orientation) · ~15 min

## Pourquoi cette leçon existe

Ce cours te demande de voyager dans l'historique de Talunor avec `git`. Avant ça, tu
dois savoir te déplacer entre les versions **en toute sécurité** — et, surtout,
distinguer *lire du vieux code* de *modifier le code actuel*. Bien comprendre ça une
fois t'évite beaucoup de confusion plus tard.

## Objectifs pédagogiques

À la fin tu sais :
- lister les versions de Talunor et en faire un checkout ;
- expliquer ce qu'est le « detached HEAD » et pourquoi il ne faut pas y commiter ;
- revenir proprement au code le plus récent ;
- nommer les quatre documents de référence et à quoi sert chacun.

## Prérequis

- `git` installé. C'est tout — pas besoin de Go pour cette leçon.

## L'idée clé : une couche = un tag

Talunor a été construit par petites étapes. Chaque étape est un **tag git** et ajoute
une capacité, avec une leçon consignée dans le `CHANGELOG.md`. Liste-les du plus
ancien au plus récent :

```bash
git tag --sort=version:refname
```

Tu verras `v0.1.0`, `v0.2.0`, … jusqu'au plus récent. `v0.1.0` est Talunor quand il
n'était qu'un magasin de mémoire — une poignée de fichiers. Les tags suivants
ajoutent le LLM, la boucle de l'agent, les outils, la sécurité, etc. Les lire dans
l'ordre, c'est regarder le projet grandir.

## Detached HEAD — lire, pas écrire

Pour regarder une ancienne version, tu fais un checkout de son tag :

```bash
git checkout v0.1.0
```

git va afficher un avertissement sur l'état **« detached HEAD »**. Ça a l'air
effrayant mais ça veut simplement dire : *tu regardes un instantané, pas une
branche.* Tu peux lire, exécuter et expérimenter librement — mais **tout commit que
tu fais ici est facile à perdre** et n'appartient pas au projet. La règle est donc :

> Pendant que tu explores un tag, **ne commite jamais**. Contente-toi de lire et
> d'exécuter.

Quand tu as fini d'explorer, reviens au code le plus récent :

```bash
git switch main
```

## Essaie

```bash
git checkout v0.1.0          # saute à la toute première couche
ls internal/                 # remarque comme il y a peu : juste memory/
git switch main              # reviens au plus récent
ls internal/                 # maintenant : agent, llm, tools, sandbox, webfetch, …
```

Ce contraste — quelques fichiers puis beaucoup — *est* l'histoire du projet.

## Les deux types de leçon

Chaque leçon est de l'un des deux types. Vérifie toujours le badge en haut :

| Badge | Ce que tu fais | Où | Commiter ? |
|-------|----------------|-----|-----------|
| 🔍 **Exploration historique** | lire comment fonctionnait une couche | `git checkout vX.Y.Z` (detached) | **Non** |
| 🛠️ **Contribution actuelle** | modifier le projet actuel | branche depuis `main` | **Oui**, sur ta branche |

Pour une leçon de contribution, tu démarres plutôt ainsi :

```bash
git switch main
git pull
git switch -c learning/mon-premier-changement   # une nouvelle branche de travail
```

## Les documents de référence

Garde-les ouverts tout au long du cours — **lis-les depuis `main`** (l'état le plus
récent), même quand tu explores un tag ancien : ils décrivent le projet actuel et
complet, et — c'est important — **les tags anciens en ont moins.**

- **`README.md`** — la visite : finalité, démarrage rapide, outils, layout. *(depuis `v0.1.0`)*
- **`CHANGELOG.md`** — le journal : chaque version avec une note *« Lessons learned »*.
  Quand tu te demandes *pourquoi* quelque chose est ainsi, regarde ici d'abord. *(depuis `v0.1.0`)*
- **`AGENTS.md`** — la carte : architecture, conventions et *pièges durement acquis*.
  *(ajouté à `v0.6.0`)*
- **`docs/atlas.md`** — une description d'une ligne de chaque fichier du dépôt.
  *(un ajout récent — seulement sur les dernières versions)*

Ce dernier point est une leçon en soi : **la documentation d'un projet grandit avec
son code.** Donc quand une leçon t'envoie vers un *tag ancien*, lis le *code* là-bas —
mais si tu as besoin de la carte ou des conventions, regarde `main`, où elles sont
complètes. Chaque leçon historique te donne aussi une petite carte « fichiers à ce
tag » pour que tu ne sois jamais perdu.

## Erreurs fréquentes

- **Commiter en étant sur un tag.** Si tu l'as fait, pas de panique : `git switch main`
  et ton commit accidentel est simplement laissé derrière (crée une branche d'abord si
  tu veux le garder).
- **Éditer des fichiers en explorant l'historique** et être surpris qu'ils
  « reviennent » quand tu passes à `main` — c'est normal ; le tag et `main` sont des
  instantanés différents.

## Checklist de complétion

- [ ] J'ai listé les tags avec `git tag --sort=version:refname`.
- [ ] J'ai fait un checkout de `v0.1.0` et vu le `internal/` plus petit.
- [ ] Je suis revenu à `main` avec `git switch`.
- [ ] Je peux expliquer, en une phrase, la différence entre une *exploration
      historique* et une *contribution actuelle*.
- [ ] Je sais à quoi sert chacun des quatre documents de référence.

**Suivant :** [Leçon 01 — Premier contact & première victoire](../01-first-contact/).
