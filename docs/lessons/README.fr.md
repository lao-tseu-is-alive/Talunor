# Talunor — un cours pratique de Go, d'agents IA et de code sûr par conception

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

Talunor est construit **une couche à la fois, chaque couche étant un tag git**
(`v0.1.0`, `v0.2.0`, …). Cet historique n'est pas qu'un journal de versions — c'est
un **cours**. Tu peux revenir à un tag ancien pour voir le projet quand il était
petit et simple, comprendre une idée isolément, puis revenir au code le plus récent.

Ce répertoire transforme cette idée en un parcours guidé. Chaque leçon a un objectif
clair, une courte liste de lecture, une expérience pratique et une checklist pour
savoir quand tu as terminé.

> **Statut : en cours.** Les leçons 00–24 sont prêtes, **entièrement en
> anglais et en français** (utilise le sélecteur de langue en haut de chaque page).
> L'Itération 5 (« mémoire véridique ») a livré les couches 20 à 24 ; la leçon 25
> (couche 24, les affirmations contestées) est la prochaine à écrire.

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

## Ce que tu vas apprendre — matrice de compétences

Le cours est un récit, mais voici la carte des *compétences* qui le sous-tendent : ce
que signifie chaque compétence, quelles leçons la construisent, le niveau à atteindre,
et où tu le prouves. Les numéros de leçon renvoient au [parcours](#le-parcours)
ci-dessous.

| Compétence | Leçons | Niveau attendu | Le prouver |
|---|---|---|---|
| **Interfaces & composition Go** | 04 · 06 · 07 | Concevoir une couture à une méthode et remplacer l'implémentation réelle par un fake | Ajouter un outil (06) et le tester avec un provider factice (07) |
| **Context, annulation & timeouts** | 04 · 16 · 19 | Expliquer la propagation du context, une opération bornée, et un arrêt propre | Lire le contrat `Close`/drain (19) ; raisonner sur un provider figé (16) |
| **Concurrence & modèle mémoire Go** | 05 · 18 · 19 | Justifier pourquoi un état partagé exige un atomic/verrou, et pourquoi une seule connexion peut en remplacer un | Lancer `go test -race` ; lire l'idée « connexion unique = verrou » (19) |
| **Persistance & récupération (SQLite + embeddings)** | 02 · 03 · 23 | Expliquer comment une requête texte devient un rappel classé et seuillé — et ce qu'un embedding ne peut pas représenter | Régler le seuil de rappel et observer les résultats changer (03) ; rendre un identifiant introuvable, puis retrouvable (23) |
| **Mémoire agentique : provenance · confiance · saillance** | 11 · 16 · 17 · 18 · 20 · 24 | Distinguer provenance d'embedding et provenance de fait ; expliquer pourquoi la confiance est assignée par le système, jamais auto-évaluée ; apprendre de l'action sans sur-affirmer | Lire les migrations 2–3 et `salience.go` (17, 18) ; tracer un fait jusqu'à son évidence avec `/why` (20) |
| **La boucle agent & les outils (ReAct)** | 05 · 06 · 13 | Tracer un appel d'outil depuis la demande du modèle jusqu'à l'observation réinjectée | Suivre un appel d'outil de bout en bout avec `/debug` (05, 06) |
| **Sécurité de l'agent : injection · policy · sandbox · SSRF** | 09 · 10 · 12 · 14 · 21 · 24 | Nommer quelle couche arrête quelle menace — et pourquoi clôturer du texte est une mitigation, pas une frontière ; décider exprès le modèle de confiance d'une mémoire, et vérifier que le code peut réellement l'appliquer | Lire la barrière policy (12), le post-mortem d'approbation (14), le modèle de confiance (21) ; reproduire une décision non appliquée et la refermer (24) |
| **Évaluation & vérification dignes de confiance** | 07 · 15 · 16 · 22 | Construire une vérification déterministe (sans juge LLM), falsifier une affirmation face au code, et savoir ce que ton vert n'a *pas* exécuté | Faire l'exercice « vérifier la revue IA » (15) ; lire les matchers (16) ; auditer les sauts de ta suite et rendre une décision privilégiée testable sans le privilège (22) |

## Le parcours

| Leçon | Sujet | Niveau | ~Durée | À lire à | Statut |
|-------|-------|--------|--------|----------|--------|
| [00](00-how-to-use-this-course/README.fr.md) | Comment utiliser ce cours | 0 · orientation | 15 min | — | ✅ prête |
| [01](01-first-contact/README.fr.md) | Premier contact & première victoire | 1 · débutant | 30 min | `v0.1.0` → `main` | ✅ prête |
| [02](02-persistent-memory/README.fr.md) | Mémoire persistante avec SQLite | 1 · débutant | 45 min | `v0.2.0` | ✅ prête |
| [03](03-semantic-recall/README.fr.md) | Rappel sémantique & embeddings | 2 · **avancé** | 60 min | `v0.2.0` | ✅ prête |
| [04](04-llm-provider-and-streaming/README.fr.md) | Provider LLM & streaming | 2 | 60 min | `v0.3.0` | ✅ prête |
| [05](05-follow-the-agent-loop/README.fr.md) | Suivre la boucle de l'agent | 2 | 60 min | `v0.4.0` → `v0.7.0` | ✅ prête |
| [06](06-build-your-first-tool/README.fr.md) | Construire ton premier outil | 2 · 🛠️ contribution | 90 min | `main` | ✅ prête |
| [07](07-test-without-a-real-llm/README.fr.md) | Tester sans vrai LLM | 2–3 · 🛠️ | 75 min | `main` | ✅ prête |
| [08](08-observability-and-errors/README.fr.md) | Observabilité & gestion d'erreurs | 2 · 🛠️ | 45 min | `main` | ✅ prête |
| [09](09-secure-web-fetching/README.fr.md) | Récupération web sécurisée (SSRF) | 3 · **avancé** | 75 min | `v0.10.0` | ✅ prête |
| [10](10-understand-the-sandbox/README.fr.md) | Comprendre le sandbox | 4 · **avancé** | 90 min | `v0.9.0` | ✅ prête |
| [11](11-when-memory-forgets/README.fr.md) | Quand la mémoire oublie en silence : provenance & observabilité | 3 · **avancé** | 75 min | `v0.11.0` → `main` | ✅ prête |
| [12](12-the-open-bar/README.fr.md) | L'open bar : pourquoi un agent a besoin d'une policy | 3 · **avancé** | 75 min | `v0.12.0` → `main` | ✅ prête |
| [13](13-plan-before-you-act/README.fr.md) | Planifier avant d'agir : du ReAct à un plan qu'on peut lire | 3 · **avancé** | 90 min | `v0.13.0` → `main` | ✅ prête |
| [14](14-the-approval-that-didnt-bind/README.fr.md) | L'approbation qui ne liait rien : post-mortem sécurité du mode plan | 3 · **avancé** | 60 min | `v0.13.1` → `main` | ✅ prête |
| [15](15-dont-trust-the-review/README.fr.md) | Ne fais pas confiance à la revue : vérifier ce qu'une IA affirme sur ton code | 2 · méta | 60 min | `main` | ✅ prête |
| [16](16-measure-the-model/README.fr.md) | Mesurer le modèle : construire un canary de fiabilité | 3 · **avancé** | 75 min | `main` | ✅ prête |
| [17](17-learning-with-humility/README.fr.md) | Apprendre avec humilité : ce que vaut un souvenir | 3 · **avancé** | 75 min | `main` | ✅ prête |
| [18](18-the-memory-of-the-gesture/README.fr.md) | La mémoire du geste : saillance, décroissance & consolidation | 3 · **avancé** | 75 min | `v0.17.0` → `main` | ✅ prête |
| [19](19-off-the-critical-path/README.fr.md) | Hors du chemin critique : apprendre en arrière-plan | 3 · **avancé** | 70 min | `v0.18.0` → `main` | ✅ prête |
| [20](20-learn-from-action/README.fr.md) | Apprendre de l'action : le « savoir des outils » est surtout interprété par le modèle | 3 · **avancé** | 65 min | `v0.19.0` → `main` | ✅ prête |
| [21](21-whose-word-counts/README.fr.md) | La parole de qui compte ? Un modèle de confiance est une décision | 3 · **avancé** | 65 min | `v0.20.0` → `main` | ✅ prête |
| [22](22-the-silent-suite/README.fr.md) | La suite silencieuse : un test sauté n'est pas un test réussi | 3 · **avancé** | 70 min | `v0.20.1` → `v0.20.2` | ✅ prête |
| [23](23-two-ways-to-find-a-memory/README.fr.md) | Deux façons de retrouver une mémoire : quand le sens est le mauvais index | 3 · **avancé** | 75 min | `v0.21.0` | ✅ prête |
| [24](24-the-adr-that-didnt-bind/README.fr.md) | L'ADR qui n'engageait rien : une décision que le code n'appliquait pas | 3 · **avancé** | 80 min | `v0.21.2` → `v0.22.0` | ✅ prête |

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
