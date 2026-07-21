# Leçon 01 — Premier contact & première victoire

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** (avec une exécution 🛠️ optionnelle sur `main`) ·
Niveau 1 (débutant) · ~30 min

## Pourquoi cette leçon existe

Le meilleur moyen de perdre un débutant, c'est une première étape qui ne marche pas.
Alors cette leçon t'offre une **vraie victoire, qui tourne, dans les dix premières
minutes** — sans LLM, sans Ollama, sans réseau — et te montre seulement ensuite
l'agent interactif. En chemin, tu verras d'où Talunor est parti et tu construiras un
premier modèle mental de l'ensemble.

## Objectifs pédagogiques

À la fin tu sais :
- lancer le smoke test mémoire hors-ligne de Talunor et lire sa sortie ;
- décrire ce qu'*est* Talunor, en un paragraphe ;
- pointer la graine du projet (`v0.1.0`) et dire ce qu'elle faisait ;
- dessiner un premier schéma d'architecture grossier.

## Prérequis

- Leçon 00 faite.
- Go 1.26+ et un compilateur C installés (`go version`, `gcc --version`).

## Étape 1 — ta première victoire (hors-ligne, sur `main`)

Depuis la racine du dépôt, sur `main` :

```bash
make deps     # unique : télécharge les extensions SQLite + le modèle d'embeddings (~52 Mo)
make doctor   # lance un smoke test hors-ligne du substrat mémoire
```

`make doctor` **n'a besoin ni de LLM ni de réseau** — les embeddings tournent en
local. Tu devrais voir quelque chose comme :

```text
✓ store open — embedding dimension = 384
• remembering corpus…
✓ stored 5 memories
• recall: "Which technology keeps a whole database in one file?"  (threshold d≤0.75)
   1. [d=0.2405] SQLite stores an entire relational database in a single file.
• recall: "Tell me about a famous French landmark."  (threshold d≤0.75)
   1. [d=0.6189] The Eiffel Tower was completed in Paris in 1889.
✓ Layers 1–2 OK: in-DB embeddings, KNN recall (thresholded), short-term buffer.
```

**C'est ça, la victoire.** Remarque : la requête *« a famous French landmark »* a
rappelé le souvenir *Eiffel Tower* alors qu'ils ne partagent aucun mot. C'est la
**recherche sémantique** — une correspondance par le *sens*, pas par mots-clés. C'est
la fondation sur laquelle tout le reste est bâti. (Tu creuseras ça en Leçon 03.)

## Étape 2 — voir d'où tout est parti (`v0.1.0`)

Maintenant, remonte à la toute première couche :

```bash
git checkout v0.1.0     # detached HEAD — lecture seule, ne commite pas (voir Leçon 00)
```

> **Fichiers à ce tag** (il n'y a pas encore de `docs/atlas.md` — cette carte est un
> ajout récent ; voici tout le projet à `v0.1.0`) :
>
> ```text
> cmd/doctor/main.go            le seul programme : mémoriser un corpus, le rappeler par le sens
> internal/memory/store.go      ouvrir SQLite, charger les extensions vector/AI, le schéma
> internal/memory/cgo_link.go   la glu cgo qui rend les extensions C chargeables
> internal/version/version.go   la constante de version
> Makefile · README.md · CHANGELOG.md
> ```

À `v0.1.0`, Talunor n'est **qu'un magasin de mémoire** — pas d'agent, pas de chat, pas
d'outils. Il n'y a même pas encore de `cmd/talunor` ; le seul programme est
`cmd/doctor`, le smoke test que tu viens de lancer. Ouvre-le :

```text
cmd/doctor/main.go      # ~un écran : ouvrir un store, mémoriser quelques faits, rappeler par le sens
```

Lis-le de haut en bas. Il est court exprès — c'est la graine dont tout l'agent a
poussé. Ton dossier `ext/` est toujours là (git l'ignore), donc tu peux même le lancer
ici :

```bash
make doctor             # marche aussi à v0.1.0 — même idée, code plus petit
```

Quand tu as fini, reviens :

```bash
git switch main
```

> **Qu'est-ce qui a changé depuis ?** L'agent interactif (`cmd/talunor`) apparaît pour
> la première fois à **`v0.4.0`**, une fois qu'il y a un LLM et une boucle d'agent à
> piloter. Tu suivras cette boucle en Leçon 05.

## Étape 3 — parler à l'agent (optionnel, nécessite Ollama)

Pour *discuter* réellement avec Talunor, il te faut un [Ollama](https://ollama.com)
local exécutant un modèle. Avec ça en place :

```bash
make run          # lance la TUI (nécessite un terminal + Ollama)
# ou, plus simple à lire pour un débutant :
go run ./cmd/talunor --plain    # un REPL en lignes, sans interface fancy
```

Tape un message, puis un autre qui s'y réfère — l'agent *se souvient* d'un tour à
l'autre parce qu'il a stocké le premier. Tape `/help` pour voir les commandes,
`/exit` pour quitter. Si tu n'as pas encore Ollama, saute cette étape ; le reste du
cours n'en a *besoin* qu'occasionnellement, et le signale toujours.

## 🛠️ Bonus optionnel — ta première contribution d'une ligne

Lire est une chose ; modifier le code et voir ta modification tourner, c'est la vraie
première étape pour contribuer. Voici la plus petite possible.

Sur `main`, `make doctor` affiche les versions des deux extensions SQLite chargées,
juste après l'ouverture du store :

```text
• AI version: 1.0.4
• vector version: 1.0.0
```

Ouvre `internal/memory/memory.go` et trouve `VersionAI` — une méthode de trois lignes
qui exécute `SELECT ai_version()` et renvoie la chaîne. C'est ton exemple modèle. Ajoute
maintenant la tienne à côté : une méthode `StoreVersion` qui renvoie la version de SQLite
lui-même via la fonction intégrée `SELECT sqlite_version()` :

```go
// StoreVersion renvoie la version de la bibliothèque SQLite sous-jacente, ex. "3.46.0".
func (s *Store) StoreVersion(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&v)
	return v, err
}
```

Puis, dans `cmd/doctor/main.go`, affiche-la à côté des deux autres lignes de version :

```go
storeVersion, err := store.StoreVersion(ctx)
if err != nil {
	return err
}
fmt.Println("• SQLite version:", storeVersion)
```

Lance `make doctor` — ta ligne apparaît. Remarque que tu n'as rien recompilé à la main :
`make doctor` utilise `go run`, donc il recompile depuis les sources à chaque fois. C'est
toute la boucle du travail sur ce code : **modifier → `go run` (via une cible `make`) → le
voir**. (Si tu avais lancé à la place un binaire `./bin/doctor` périmé, compilé *avant* ta
modification, ta ligne n'apparaîtrait pas — le piège classique du « pourquoi rien ne se
passe ? ».)

## Modèle mental

D'après ce que tu as vu, Talunor ressemble à ça :

```text
Toi  (terminal : TUI, ou REPL --plain)
  │
  ▼
Agent  — un « tour » : rappeler des souvenirs, interroger le LLM, éventuellement un outil, mémoriser
  │
  ├─► Mémoire  (SQLite : tes tours + faits, cherchés par le sens)
  ├─► LLM      (Ollama ou OpenRouter — la « réflexion »)
  └─► Outils   (calculatrice, horloge, recherche mémoire, et bash / web_fetch en opt-in)
```

Garde cette image ; les prochaines leçons zooment sur chaque case.

## Erreurs fréquentes

- **Sauter `make deps`.** Sans ça, `make doctor` ne trouve ni les extensions ni le
  modèle. Lance-le une fois.
- **Attendre que `make run` marche sans Ollama.** Le chat a besoin d'un modèle ; la
  victoire hors-ligne (`make doctor`) non.
- **Commiter en étant sur `v0.1.0`.** Tu es en detached HEAD — lecture seule.

## Checklist de complétion

- [ ] `make doctor` a tourné et j'ai vu la sortie de rappel.
- [ ] Je peux expliquer, en une phrase, pourquoi *« French landmark »* a rappelé
      *Eiffel Tower* (recherche sémantique).
- [ ] J'ai lu `cmd/doctor/main.go` à `v0.1.0` et je suis revenu à `main`.
- [ ] Je peux dire ce que faisait `v0.1.0` et ce qu'a ajouté `v0.4.0`.
- [ ] Je peux dessiner le modèle mental à quatre cases de mémoire.
- [ ] *(optionnel)* J'ai ajouté une méthode `StoreVersion` et vu ma ligne
      `• SQLite version:` apparaître dans `make doctor`.

**Suivant :** [Leçon 02 — Mémoire persistante avec SQLite](../02-persistent-memory/).
