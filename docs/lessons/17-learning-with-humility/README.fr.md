# Leçon 17 — Apprendre avec humilité : ce que vaut un souvenir

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration + pratique** (lire `internal/memory` et `internal/agent` sur `main`) ·
Niveau 3 (avancé) · ~75 min

## Pourquoi cette leçon existe

Depuis la Leçon 05, Talunor *se souvient* : il distille des faits durables de ce que
tu dis et les rappelle plus tard. Mais un souvenir n'est utile que si tu sais *à quel
point lui faire confiance*. Un fait que l'utilisateur a énoncé clairement (« je
m'appelle Carlos ») mérite plus de poids qu'un fait que le modèle a *inféré*, et l'un
comme l'autre méritent moins de poids quand le modèle qui distille est lui-même peu
fiable.

L'Itération 4 porte sur l'apprentissage, et son premier geste (Layer 16) est
l'humble : avant de faire apprendre l'agent *davantage*, le faire apprendre
*honnêtement*. Chaque souvenir enregistre désormais **d'où il vient** et **à quel
point s'y fier** — et, surtout, cette confiance est assignée par le *système* depuis
la source, jamais auto-rapportée par le modèle. Cette leçon porte sur ce mécanisme,
sur pourquoi chaque choix est ce qu'il est, et sur comment il referme la boucle avec
la calibration bâtie en Leçon 16.

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer pourquoi un souvenir stocké a besoin de **provenance** et de **confiance**,
  pas juste de texte ;
- énoncer la règle porteuse — **la confiance vient de la source, jamais de l'auto-report
  du modèle** — et pourquoi la confiance auto-rapportée est un piège ;
- décrire le **lien calibration** : comment la confiance d'un fait appris est scalée par
  la fiabilité *mesurée* du modèle, et pourquoi c'est câblé comme un scalaire découplé ;
- lire comment une nouvelle colonne a atteint le schéma via une **migration** ordonnée ;
- poser les leviers et voir un fait appris (et rappelé) avec confiance.

## Prérequis

- **Leçon 05** (la boucle de l'agent) et l'étape de réflexion qu'elle a introduite.
- **Leçon 16** (mesurer le modèle) — celle-ci consomme un score de calibration.
- **Leçons 12 / 15** — l'instinct « ne fais pas confiance au jugement du modèle »,
  appliqué maintenant à la *confiance* du modèle elle-même.

## Partie 1 — un souvenir, c'est plus que son texte

Lis la forme d'un souvenir sur `main` :

```text
internal/memory/memory.go
```

Trouve `Provenance` et `BaseConfidence`. Une `Provenance` dit d'où vient un souvenir :

- `user_stated` — ancré dans les mots de l'utilisateur (un tour user, ou un fait
  distillé de ce que l'utilisateur a dit) ;
- `model_inferred` — le modèle l'a produit (un tour assistant, ou une inférence) ;
- `tool_observed` — un résultat d'outil vérifié ;
- `unspecified` — legacy ou non classé.

`BaseConfidence(p)` mappe chacune vers une confiance de départ : un résultat d'outil
vérifié (0.95) dépasse une déclaration utilisateur (0.9), qui dépasse une inférence du
modèle (0.5). C'est ce que vaut la *source*, avant tout le reste.

Maintenant — d'où viennent les colonnes `provenance` et `confidence` ? Elles n'étaient
pas dans la table d'origine. Lis :

```text
internal/memory/migrate.go
```

C'est la machinerie du Layer 15 en action. Le schéma évolue via une liste de migrations
**ordonnée, append-only** ; la version appliquée est un entier unique dans la table
`meta`. La migration 1 est la baseline (la table `memories`) ; la **migration 2** ajoute
`provenance` et `confidence`. Chaque migration tourne une fois, dans sa propre
transaction avec son stamp de version, donc mettre à niveau un DB existant est
automatique et résistant au crash. La seule règle qui rend cela fiable : *append only —
ne jamais réordonner, renuméroter, ou éditer une migration livrée ; corrige une erreur
par une nouvelle.* Une liste de migrations est un historique que chaque base du monde a
déjà exécuté.

## Partie 2 — la règle porteuse : la confiance vient de la source, pas du modèle

Voici la décision qui compte le plus, et il est facile de la rater. Quand l'agent
distille un fait, à quel point doit-il en être sûr ? La réponse tentante est de
*demander au modèle* : « quelle est ta confiance dans ce fait ? » **Ne le fais pas.**

> **L'idée centrale.** La confiance auto-rapportée d'un modèle n'est *pas calibrée* —
> c'est la même sortie fluide et plausible que tout le reste de ce qu'il dit, et
> (Leçon 15) un modèle est souvent le plus confiant quand il a le plus tort. Une
> confiance que tu as *demandée* n'est qu'une affirmation de plus. Donc Talunor ne
> demande jamais. La confiance est assignée par le **système** depuis *quel pipeline a
> produit le souvenir* : un fait distillé du message user est `user_stated` ; un tour
> de l'assistant est `model_inferred`. Objectif, pas auto-noté.

Lis comment la réflexion stocke un fait — `reflect` dans :

```text
internal/agent/agent.go
```

Remarque qu'elle appelle `store.RememberFact(ctx, fact, ProvenanceUserStated, conf)` —
la provenance est fixée par le pipeline (les faits viennent du message user), et `conf`
est calculée par l'agent, pas renvoyée par le modèle. C'est le même principe que la
policy de la Leçon 12 et la vérification de la Leçon 15, tourné vers l'intérieur : *ne
fais pas confiance au jugement du modèle sur sa propre fiabilité — décide-le de
l'extérieur.*

## Partie 3 — le lien calibration

Un fait `user_stated` démarre à 0.9 de confiance. Mais il est quand même passé par le
*modèle d'extraction*, qui a pu confabuler un « fait » que tu n'as jamais énoncé. À quel
point cela doit-il tempérer la confiance ? Exactement autant que le modèle est
**mesurablement** peu fiable — ce que la calibration de la Leçon 16 te donne.

Lis le scaling dans `reflect` :

```go
conf := clamp01(memory.BaseConfidence(memory.ProvenanceUserStated) * a.cfg.ModelConfidence)
```

`Config.ModelConfidence` (depuis `TALUNOR_MODEL_CONFIDENCE`, défaut 1.0) est un scalaire
dans [0,1] — la fiabilité du modèle, que tu obtiens du pass-rate global d'un run
`cmd/calibrate`. Un modèle qui a scoré 0.7 sur ta suite → pose
`TALUNOR_MODEL_CONFIDENCE=0.7` → ses faits appris atterrissent à `0.9 × 0.7 = 0.63` au
lieu de `0.9`.

> **Pourquoi un scalaire, pas une API.** L'agent ne *lance pas* la calibration — cela
> coupler ait deux sous-systèmes et ralentirait chaque tour. Il consomme un *chiffre*
> qu'un opérateur pose depuis un run `calibrate` séparé. Toute la promesse
> « apprentissage informé par la calibration », livrée par une multiplication et zéro
> couplage. L'intégration la moins chère entre deux systèmes est souvent un chiffre, pas
> un appel.

Et l'autre côté : `Config.RecallMinConfidence` (`TALUNOR_RECALL_MIN_CONFIDENCE`, défaut
0 = off) écarte du recall les souvenirs sous un seuil, pour qu'un « fait » basse-confiance
ne soit pas réinjecté dans le prompt comme s'il était établi. Apprends prudemment ;
rappelle prudemment.

## Partie 4 — regarde-le apprendre avec humilité

D'abord les tests déterministes — sans modèle :

```bash
go test ./internal/memory/ -run 'Fact|Migrate' -v
```

Lis `TestFactProvenanceAndConfidence` à côté du code : il stocke un fait avec une
provenance et une confiance explicites et vérifie que le recall les porte, et confirme
que la provenance d'un tour est dérivée de son rôle.

Maintenant en live (nécessite Ollama). Lance avec une confiance-modèle *délibérément*
basse, dis un fait durable à l'agent, puis inspecte la mémoire :

```bash
TALUNOR_MODEL_CONFIDENCE=0.5 go run ./cmd/talunor --plain
```

```text
you> my name is Carlos and I work in Go
you> /list
```

`/list` montre le fait appris avec `(user_stated 45%)` — base 0.9 × ton 0.5. Pose
`TALUNOR_MODEL_CONFIDENCE=1.0` (ou laisse-le non défini) et le même fait est appris à
90%. Tu viens de faire *douter* l'agent de ce qu'il apprend, en proportion exacte de ta
confiance dans le modèle.

Enfin, referme la boucle avec la Leçon 16 : lance `cmd/calibrate` contre ton modèle,
prends le pass-rate global, et pose `TALUNOR_MODEL_CONFIDENCE` dessus. Là le chiffre
n'est plus une supposition — il est *mesuré*. Optionnellement, pose
`TALUNOR_RECALL_MIN_CONFIDENCE=0.5` et regarde les faits basse-confiance cesser de
revenir au recall.

## Les principes

```text
Apprends à proportion de ce en quoi tu peux avoir confiance ; ne laisse jamais l'apprenant noter sa propre confiance.
```

1. **Un souvenir est une métadonnée, pas juste du texte.** La provenance (source) et la
   confiance (fiabilité) sont ce qui permet au recall — et au modèle — de le pondérer.
2. **La confiance vient de la source, jamais de l'auto-report du modèle.** Une confiance
   demandée n'est qu'une affirmation non calibrée de plus.
3. **La fiabilité mesurée scale ce que tu apprends.** Un score de calibration, consommé
   comme scalaire découplé, empêche les faits d'un modèle peu fiable de gagner de l'autorité.
4. **Fais évoluer le schéma en ajoutant une migration, jamais en éditant une livrée.**

## Checklist de fin

- [ ] Je sais nommer les quatre provenances et expliquer pourquoi outil > user > inférence.
- [ ] Je sais pourquoi l'agent ne demande jamais au modèle sa confiance.
- [ ] J'ai lu `reflect` et je peux pointer la ligne `BaseConfidence × ModelConfidence`.
- [ ] Je sais pourquoi la calibration est câblée en scalaire, pas en appel d'API.
- [ ] J'ai lu la migration 2 et je sais énoncer la règle append-only.
- [ ] J'ai lancé l'agent avec `TALUNOR_MODEL_CONFIDENCE=0.5` et vu la confiance réduite.
- [ ] Je sais ce contre quoi `TALUNOR_RECALL_MIN_CONFIDENCE` protège.

---

## 🎓 À propos de cette leçon

C'est la première leçon d'apprentissage de l'Itération 4, et elle mène délibérément par
l'*humilité* : l'agent gagne la capacité de se souvenir avec une confiance graduée avant
de gagner celle de se souvenir davantage. Remarque l'arc qu'elle complète — la Leçon 11 a
attrapé un substrat se dégradant en silence, la Leçon 16 a mesuré la fiabilité d'un
modèle, et cette leçon *dépense* cette mesure, la laissant gouverner ce que l'agent a le
droit de croire. La couche suivante bâtit directement sur cette métadonnée : **salience et
décroissance** — non pas « à quel point faire confiance à un souvenir » mais « quels
souvenirs garder, renforcer, ou laisser s'estomper ». La confiance d'abord ; la rétention
ensuite.

Retour à l'[index du cours](../).
