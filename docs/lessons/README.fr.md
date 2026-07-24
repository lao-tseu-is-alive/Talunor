# Talunor — un cours pratique de Go, d'agents IA et de code sûr par conception

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

Talunor est construit **une couche à la fois, chaque couche étant un tag git**
(`v0.1.0`, `v0.2.0`, …). Cet historique n'est pas qu'un journal de versions — c'est
un **cours**. Tu peux revenir à un tag ancien pour voir le projet quand il était
petit et simple, comprendre une idée isolément, puis revenir au code le plus récent.

Ce répertoire transforme cette idée en un parcours guidé. Chaque leçon a un objectif
clair, une courte liste de lecture, une expérience pratique et une checklist pour
savoir quand tu as terminé.

> **Statut : complet.** Les vingt leçons (00–19) sont prêtes, **entièrement en
> anglais et en français** (utilise le sélecteur de langue en haut de chaque page).

## Pour qui

Des développeurs qui connaissent un peu la programmation et veulent apprendre, en
lisant et en exécutant du vrai code :
- **Go** — ses interfaces, ses channels, ses tests, ses idiomes.
- **les agents IA** — mémoire, rappel, la boucle raisonnement→action, outils, approbation.
- **le code sûr par conception** — validation des entrées, SSRF, sandboxing, chaîne
  d'approvisionnement.

**Tu n'as pas besoin de bien connaître Go.** Si Go est tout nouveau pour toi, passe
une heure sur [A Tour of Go](https://go.dev/tour) d'abord — c'est suffisant pour
suivre. Certaines leçons sont marquées **Avancé** ; il est tout à fait normal de
t'arrêter avant et d'y revenir plus tard.

## Prérequis

- **Go 1.26+** et un **compilateur C** (gcc/clang) — Talunor utilise cgo.
- **git**, et une machine **Linux x86_64** (le chemin le plus fluide).
- **Ollama** n'est nécessaire qu'à partir de l'étape *optionnelle* de la Leçon 01 —
  la première victoire se fait entièrement hors-ligne.

Mise en place unique (télécharge les extensions SQLite + le modèle d'embeddings,
~52 Mo) :

```bash
git clone https://github.com/lao-tseu-is-alive/Talunor.git
cd Talunor
make deps
make doctor   # ta première victoire — le substrat mémoire, en local, hors-ligne
```

## Le parcours

| Leçon | Sujet | Niveau | ~Durée | À lire à | Statut |
|-------|-------|--------|--------|----------|--------|
| [00](00-how-to-use-this-course/) | Comment utiliser ce cours | 0 · orientation | 15 min | — | ✅ prête |
| [01](01-first-contact/) | Premier contact & première victoire | 1 · débutant | 30 min | `v0.1.0` → `main` | ✅ prête |
| [02](02-persistent-memory/) | Mémoire persistante avec SQLite | 1 · débutant | 45 min | `v0.2.0` | ✅ prête |
| [03](03-semantic-recall/) | Rappel sémantique & embeddings | 2 · **avancé** | 60 min | `v0.2.0` | ✅ prête |
| [04](04-llm-provider-and-streaming/) | Provider LLM & streaming | 2 | 60 min | `v0.3.0` | ✅ prête |
| [05](05-follow-the-agent-loop/) | Suivre la boucle de l'agent | 2 | 60 min | `v0.4.0` → `v0.7.0` | ✅ prête |
| [06](06-build-your-first-tool/) | Construire ton premier outil | 2 · 🛠️ contribution | 90 min | `main` | ✅ prête |
| [07](07-test-without-a-real-llm/) | Tester sans vrai LLM | 2–3 · 🛠️ | 75 min | `main` | ✅ prête |
| [08](08-observability-and-errors/) | Observabilité & gestion d'erreurs | 2 · 🛠️ | 45 min | `main` | ✅ prête |
| [09](09-secure-web-fetching/) | Récupération web sécurisée (SSRF) | 3 · **avancé** | 75 min | `v0.10.0` | ✅ prête |
| [10](10-understand-the-sandbox/) | Comprendre le sandbox | 4 · **avancé** | 90 min | `v0.9.0` | ✅ prête |
| [11](11-when-memory-forgets/) | Quand la mémoire oublie en silence : provenance & observabilité | 3 · **avancé** | 75 min | `v0.11.0` → `main` | ✅ prête |
| [12](12-the-open-bar/) | L'open bar : pourquoi un agent a besoin d'une policy | 3 · **avancé** | 75 min | `v0.12.0` → `main` | ✅ prête |
| [13](13-plan-before-you-act/) | Planifier avant d'agir : du ReAct à un plan qu'on peut lire | 3 · **avancé** | 90 min | `v0.13.0` → `main` | ✅ prête |
| [14](14-the-approval-that-didnt-bind/) | L'approbation qui ne liait rien : post-mortem sécurité du mode plan | 3 · **avancé** | 60 min | `v0.13.1` → `main` | ✅ prête |
| [15](15-dont-trust-the-review/) | Ne fais pas confiance à la revue : vérifier ce qu'une IA affirme sur ton code | 2 · méta | 60 min | `main` | ✅ prête |
| [16](16-measure-the-model/) | Mesurer le modèle : construire un canary de fiabilité | 3 · **avancé** | 75 min | `main` | ✅ prête |
| [17](17-learning-with-humility/) | Apprendre avec humilité : ce que vaut un souvenir | 3 · **avancé** | 75 min | `main` | ✅ prête |
| [18](18-the-memory-of-the-gesture/) | La mémoire du geste : saillance, décroissance & consolidation | 3 · **avancé** | 75 min | `v0.17.0` → `main` | ✅ prête |
| [19](19-off-the-critical-path/) | Hors du chemin critique : apprendre en arrière-plan | 3 · **avancé** | 70 min | `v0.18.0` → `main` | ✅ prête |

## Deux types de leçon — à ne pas confondre

Chaque leçon est de l'un des deux types, indiqué en haut par un badge :

**🔍 Exploration historique** — tu fais un `git checkout` d'un tag ancien pour *lire*
comment était Talunor à ce stade. Tu es en « detached HEAD ». **Ne commite jamais
ici.** Quand tu as fini, `git switch main` pour revenir.

**🛠️ Contribution actuelle** — tu modifies le projet *actuel*. Pars toujours de
`main` et crée une branche :
`git switch main && git pull && git switch -c learning/mon-changement`.

La Leçon 00 explique ça en détail ; c'est le seul point qui piège vraiment les gens.

## Les documents de référence

Garde-les ouverts au fur et à mesure — **lis-les depuis `main`** (les tags anciens en
ont moins ; la Leçon 00 explique pourquoi, et chaque leçon historique cartographie
son propre tag) :

- **[README.md](../../README.md)** — ce qu'est Talunor, démarrage rapide, outils, layout.
- **[CHANGELOG.md](../../CHANGELOG.md)** — le journal couche par couche avec une section
  *« Lessons learned »* par version. C'est le cœur du projet.
- **[AGENTS.md](../../AGENTS.md)** — la carte : architecture, conventions, pièges.
- **[docs/atlas.md](../atlas.md)** — une description d'une ligne de chaque fichier
  (versions récentes).

## Comment travailler une leçon

1. Lis *Pourquoi cette leçon existe* et *Objectifs pédagogiques*.
2. Fais le checkout (ou la branche) demandé.
3. Lis les fichiers listés — inutile de tout lire ligne par ligne ; vise la *forme*.
4. Lance les commandes et fais l'expérience.
5. Coche la **checklist de complétion**. Si toutes les cases sont cochées, passe à la
   suite.

Prends ton temps. Le but n'est pas la vitesse — c'est d'être capable d'*expliquer*
comment chaque pièce fonctionne et pourquoi elle a été construite ainsi.
