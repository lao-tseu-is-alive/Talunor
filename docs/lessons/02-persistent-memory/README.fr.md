# Leçon 02 — Mémoire persistante avec SQLite

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 1 (débutant) · ~45 min

## Pourquoi cette leçon existe

Un agent qui oublie tout entre deux messages n'est qu'un chatbot. La caractéristique
qui définit Talunor, c'est une **mémoire qui persiste** — d'un tour à l'autre et d'une
session à l'autre. Cette leçon ouvre la boîte : comment Talunor *stocke*-t-il les
choses, et qu'est-ce qui survit exactement à un redémarrage ? Tu liras la couche de
stockage quand elle était encore petite, à **`v0.2.0`**.

## Objectifs pédagogiques

À la fin tu sais :
- expliquer de quoi le `memory.Store` est responsable (et ce qu'il cache
  délibérément) ;
- décrire le cycle de vie du store : `Open` → utilisation → `Close` ;
- distinguer le **tampon court terme** du **store long terme**, et dire ce qui survit
  à un redémarrage.

## Prérequis

- Leçons 00 et 01 faites.

## Checkout de la couche mémoire

```bash
git checkout v0.2.0     # detached HEAD — lecture seule (voir Leçon 00)
```

> **Fichiers à ce tag** (pas encore de `docs/atlas.md` — voici tout le projet) :
>
> ```text
> cmd/doctor/main.go             le seul programme : stocker un corpus, le rappeler
> internal/memory/store.go       Open/Close la DB, charger les extensions C, le schéma, Embed
> internal/memory/memory.go      Remember / Recall / Count — l'API mémoire
> internal/memory/shortterm.go   un petit ring buffer des tours les plus récents
> internal/memory/cgo_link.go    la glu cgo qui rend les extensions C chargeables
> Makefile · README.md · CHANGELOG.md
> ```
>
> Il n'y a pas encore d'agent ni de LLM — Talunor est encore juste une mémoire à
> laquelle on parle depuis un smoke test.

## Lis, dans cet ordre

```text
internal/memory/store.go       # commence ici : Open, Close, le schéma, Dim, Embed
internal/memory/memory.go      # puis l'API : Remember, Recall, Count
internal/memory/shortterm.go   # enfin le tampon en RAM des tours récents
```

## Les deux types de mémoire

C'est la distinction clé à retenir :

| | Court terme | Long terme |
|--|-------------|------------|
| Où | `shortterm.go` — un slice en RAM | `store.go` — un fichier SQLite sur disque |
| Contient | les derniers tours, verbatim | tout ce que tu as déjà mémorisé |
| Survit à un redémarrage ? | **Non** | **Oui** |
| But | contexte immédiat, pas cher | rappel par le sens, plus tard |

Le tampon court terme est un **ring buffer** : il ne garde que les *N* derniers tours
et jette les plus anciens — assez pour garder une conversation cohérente sans grossir
à l'infini. Le store long terme est un unique fichier SQLite, ce qui explique pourquoi
le vrai agent (Leçon 01) savait encore des choses après que tu aies quitté et relancé.

## Le store est une frontière, pas juste « du SQL caché »

Regarde `Open` et `Close` dans `store.go`. Une abstraction de store fait plus que
cacher du SQL — elle définit des **garanties** :

- *quand* le schéma est-il créé ? (`Open` l'amorce) ;
- *que* se passe-t-il en cas d'échec ? (`Remember`/`Recall` renvoient une `error` — le
  code appelant décide) ;
- *que* faut-il libérer ? (`Close`, pour rendre le handle de fichier et les ressources
  C).

C'est la vraie leçon de cette couche : **une ressource a un cycle de vie, et une bonne
abstraction rend ce cycle explicite.**

## Expérience

Lance le smoke test (il utilise la couche mémoire de bout en bout) :

```bash
make doctor
```

Il `Remember` un petit corpus, puis le `Recall`. Lis `cmd/doctor/main.go` en parallèle
de la sortie : trouve les appels `Remember(...)` et l'appel `Recall(...)`, et
associe-les à ce qui s'affiche.

Puis une expérience de pensée — trace-la dans le code, ne l'exécute pas :

```text
Remember("My preferred language is Go")   → écrit dans SQLite (survit au redémarrage)
tampon court terme                         → le garde en RAM   (perdu au redémarrage)
[ redémarre le programme ]
Recall("what language do I like?")         → le retrouve — il était sur disque
```

Quand tu as fini, reviens au code le plus récent :

```bash
git switch main
```

## Questions auxquelles répondre

- Quelle fonction crée le schéma de la base, et quand s'exécute-t-elle ?
- Si `Remember` échoue, qui décide quoi faire — le store, ou son appelant ? Pourquoi
  est-ce le bon endroit ?
- Après un redémarrage, que sait encore l'agent, et qu'a-t-il oublié ?

## Erreurs fréquentes

- **Confondre les deux mémoires.** « Il se souvient dans un chat » (court terme) n'est
  pas la même chose que « il se souvient entre redémarrages » (long terme SQLite).
- **Oublier `Close`.** En Go, une ressource que tu `Open` doit être `Close` (souvent
  avec `defer`). Remarque où les appelants le font.

## Checklist de complétion

- [ ] Je peux dire, en une phrase, de quoi `memory.Store` est responsable.
- [ ] J'ai trouvé `Open` et `Close` et je peux décrire le cycle de vie du store.
- [ ] Je peux nommer une chose qui survit à un redémarrage et une qui ne survit pas.
- [ ] J'ai lancé `make doctor` et associé un appel `Remember`/`Recall` à sa sortie.
- [ ] Je suis revenu à `main`.

**Suivant :** [Leçon 03 — Rappel sémantique & embeddings](../03-semantic-recall/).
