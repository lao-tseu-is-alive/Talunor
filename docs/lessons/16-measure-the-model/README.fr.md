# Leçon 16 — Mesurer le modèle : construire un canary de fiabilité

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration + pratique** (lire et exécuter `internal/calibration` sur `main`) ·
Niveau 3 (avancé) · ~75 min

## Pourquoi cette leçon existe

La Leçon 15 t'a appris à vérifier *une* affirmation, *une fois*, à la main : exiger la
ligne exacte, la recouper contre la vérité terrain. Mais tu ne peux pas hand-checker
chaque modèle à chaque mise à jour — les providers livrent de nouvelles versions, des
variantes « flash » moins chères, des changements silencieux, et un modèle en qui tu
avais confiance la semaine dernière peut se dégrader en silence aujourd'hui.

Cette leçon est le pendant ingénierie : **automatiser la vérification, et la lancer en
continu.** La Couche 14 (`v0.14.0`) a ajouté un petit harnais — `internal/calibration`
+ `cmd/calibrate` — qui exécute une suite fixe de scénarios à réponse connue et note à
quel point un modèle les réussit fiablement. C'est un *canary de fiabilité*, et en
construire un est une compétence réutilisable bien au-delà de Talunor.

L'important n'est pas le CLI. Ce sont les **trois décisions de conception** qui rendent
un harnais d'évaluation digne de confiance — celles qui reviennent dans *toute* éval
sérieuse de LLM, et que la plupart des gens ratent.

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer l'invariant porteur — **le vérificateur doit être déterministe, jamais un
  LLM** — et le piège récursif qui découle de sa violation ;
- séparer les **deux axes de la fiabilité**, justesse et consistance, et dire pourquoi
  un pass-rate proche de 0.5 est le cas dangereux, et où l'écart-type a réellement sa
  place ;
- expliquer pourquoi la valeur d'une calibration est la **dérive par rapport à une
  baseline**, pas le score absolu, et énoncer le modèle de menace honnête d'une suite
  de test partagée ;
- lancer le harnais, écrire un scénario, et attraper une régression contre une baseline.

## Prérequis

- **Leçon 15** (ne fais pas confiance à la revue) — celle-ci automatise la vérification
  manuelle qu'elle enseignait, et partage sa thèse : *la sortie d'une IA est une
  affirmation, jamais une preuve.*
- **Leçon 11** (provenance des embeddings) — la même idée de *canary*, appliquée à la
  véracité d'un modèle au lieu d'un espace vectoriel.
- Un peu de Go, et (pour les parties live) un Ollama qui tourne.

## Partie 1 — l'invariant porteur : aucun juge LLM

Lis la couche des matchers sur `main` :

```text
internal/calibration/assert.go
```

Un `Assert` est un ensemble de vérifications **déterministes** sur la réponse du modèle
: `equals`, `contains`, `regex`, `number` (avec tolérance), `json_valid`,
`any_of`/`all_of`. Chacun est une fonction pure de la chaîne de réponse. Lis la doc du
type, et remarque ce qui est *absent* : il n'y a aucun matcher « demander à un autre
modèle si cette réponse est juste » — volontairement.

> **L'idée centrale.** Un harnais de calibration mesure si un modèle est fiable. Si le
> harnais *jugeait* les réponses avec un modèle, la mesure hériterait de l'exacte
> irfiabilité qu'elle existe pour attraper — un juge confiant-mais-faux notant une
> réponse confiante-mais-fausse. La vérité terrain doit être **vérifiable par une
> machine**, ou elle n'entre pas dans un scénario. C'est le principe de la Leçon 15,
> transformé en règle d'architecture.

C'est la décision que la plupart des frameworks d'éval ratent : le « LLM-as-judge » est
pratique et passe à l'échelle sur des réponses ouvertes, mais il *blanchit* le problème
au lieu de le résoudre. La contrainte déterministe est une vraie limite — elle impose
des scénarios à réponse vérifiable (arithmétique, format, faits exacts, résultats
d'outil) — et cette limite est le prix d'un chiffre digne de confiance. Ouvre maintenant :

```text
internal/calibration/scenario.go
```

Un `Scenario` fait 1–5 tours, chacun un message user plus son `Assert`. Note que `Parse`
est **source-agnostique** — il prend des bytes, pas un chemin, donc d'où vient la suite
(clair, fichier privé, blob déchiffré) ne regarde pas le parser. Et note que les
scénarios ne portent **aucune mémoire de session** : le runner construit chaque
conversation à partir des tours seuls, donc chaque modèle est testé clean-room, et la
*même* suite tourne à l'identique contre n'importe quel `llm.Provider`.

## Partie 2 — deux axes : justesse et consistance

Lis le runner et sa métrique :

```text
internal/calibration/runner.go
internal/calibration/metrics.go
```

`Run` rejoue chaque scénario **N fois**. Pourquoi répéter, si la réponse est fixe ?
Parce que le *modèle* n'est pas déterministe : à température non nulle, le même prompt
produit des réponses différentes. Un scénario a donc deux modes d'échec indépendants,
et il faut mesurer les deux :

- **Justesse** — *réussit-il ?* La moyenne sur les runs : le **pass-rate**.
- **Consistance** — *réussit-il de façon fiable ?* La dispersion sur les runs.

Voici le point subtil, et c'est une erreur de stats fréquente. Pour un résultat
**binaire** pass/fail, la variance est fixée par le pass-rate (c'est un Bernoulli :
variance = p(1−p)). Un « écart-type du pass/fail » séparé n'apprend donc rien de neuf —
le signal de consistance *est* la distance du pass-rate à 0 ou 1 :

- `1.0` → fiablement juste ;
- `0.0` → fiablement faux (mauvais, mais au moins prévisible) ;
- **`~0.5` → flaky** — juste une fois sur deux. C'est le résultat le plus dangereux,
  car un seul run de test pourrait montrer l'une ou l'autre face.

L'écart-type ne gagne sa place que sur une métrique **continue** — c'est pourquoi
`metrics.go` calcule `mean ± stddev` pour la **latence**, pas pour le pass/fail.
Rapporter un écart-type d'un résultat binaire serait du bruit déguisé en rigueur ;
rapporter la distance du pass-rate aux extrêmes est le vrai signal de consistance.
Placer *quelle statistique va où* correctement fait la différence entre un harnais qui
révèle le flakiness et un qui le cache.

## Partie 3 — la valeur, c'est la dérive, pas le score

Lis :

```text
internal/calibration/baseline.go
```

`AsBaseline` fige les pass-rates d'un run ; `Diff` compare un run ultérieur et signale
tout scope (overall / catégorie / scénario) qui a chuté au-delà d'un seuil. Cette
comparaison — pas le chiffre absolu — est le propos.

> **Pourquoi la dérive, pas l'absolu.** Une suite publique peut être *mémorisée* : si
> les scénarios vivent dans un repo public, un provider a pu s'entraîner dessus, donc
> un score absolu élevé pourrait vouloir dire « il a appris les réponses », pas « il
> est capable ». Mais la *même* suite rejouée dans le temps, contre le *même* provider,
> transforme « le modèle s'est dégradé en silence » en build rouge. C'est le canary
> d'embedding de la Leçon 11, pointé sur la véracité : tu détectes le *changement*
> avant tes utilisateurs.

Lis maintenant le modèle de menace sur lequel le harnais est honnête — l'en-tête de la
suite seed et la doc du chiffrement :

```text
docs/calibration.seed.yaml          # l'en-tête « Threat model (read this) »
internal/calibration/crypt.go       # AES-256-GCM optionnel, et ses limites honnêtes
```

Deux limites honnêtes à intérioriser, car sur-croire le chiffre est un échec en soi :

1. **Suite publique ⇒ absolu faible, relatif fort.** Livre un jeu d'exemple public
   (pour l'enseignement et la dérive), garde une suite *privée* pour une mesure
   absolue fiable — gitignorée, ou chiffrée (`CALIBRATION_KEY`) si tu dois la versionner
   dans un repo partagé.
2. **Le chiffrement protège des scrapers, pas du provider.** Avec un modèle *hébergé*
   tu lui tends les prompts déchiffrés à l'inférence, il peut donc les récolter. Le
   chiffrement stoppe le scraping passif du repo ; seul un modèle **local** + une suite
   privée est totalement privé. Nommer ce qu'un contrôle ne couvre *pas* est aussi
   important que le contrôle.

## Partie 4 — lance-le, et attrape une régression

D'abord les tests déterministes — sans modèle :

```bash
go test ./internal/calibration/ -v
```

Maintenant en live (nécessite Ollama). Lance la suite seed :

```bash
go run ./cmd/calibrate --suite docs/calibration.seed.yaml
```

Tu verras un pass-rate par scénario et par catégorie, et `latency mean ± stddev`.
**Écris maintenant ton propre scénario** — une suite d'un fichier testant quelque chose
qui t'importe :

```yaml
# my.yaml
suite: mine
scenarios:
  - id: strict-format
    category: format
    runs: 3
    turns:
      - user: "Reply with exactly the word OK, uppercase, nothing else."
        expect: { equals: "OK" }
```

```bash
go run ./cmd/calibrate --suite my.yaml
```

Si le modèle ajoute de la ponctuation ou de la prose, le pass-rate tombe sous 1.0 — un
vrai signal de suivi d'instruction. Enfin, sens le mécanisme de **dérive** : sauve une
baseline, puis compare un run ultérieur :

```bash
go run ./cmd/calibrate --suite my.yaml --save-baseline base.json
go run ./cmd/calibrate --suite my.yaml --baseline base.json   # exit 1 s'il a régressé
```

Pointe `--baseline` sur un snapshot sauvé et le CLI sort non-zéro sur une régression —
le hook que tu mettrais devant une mise à jour de modèle en CI. Change de modèle
(`TALUNOR_MODEL=…`) et relance contre la même baseline pour voir un modèle plus faible
le faire trébucher.

## Les principes

```text
Tu ne peux pas gouverner ce que tu ne mesures pas — et tu ne peux pas mesurer un LLM avec un LLM.
```

1. **Le vérificateur doit être déterministe.** Dès qu'un modèle juge la sortie, la
   mesure hérite du défaut qu'elle a été construite pour attraper.
2. **Justesse et consistance sont des axes différents.** Lis la distance du pass-rate à
   0/1 pour un check binaire ; réserve l'écart-type à une métrique continue.
3. **Mesure la dérive, pas l'absolu.** Une baseline pinée transforme la dégradation
   silencieuse en signal ; le score absolu d'une suite publique est mou.
4. **Sois honnête sur ce que le harnais (et son chiffrement) ne couvre pas.** Un chiffre
   sur-cru est pire qu'aucun chiffre.

## Checklist de fin

- [ ] Je sais expliquer pourquoi un vérificateur de calibration ne doit pas être un LLM.
- [ ] J'ai lu `assert.go` et je sais nommer les matchers déterministes.
- [ ] Je sais pourquoi un pass-rate proche de 0.5 est pire que 0.0, et où va un écart-type.
- [ ] J'ai lu `baseline.go` et je sais expliquer dérive vs score absolu.
- [ ] J'ai lancé la suite seed et écrit mon propre scénario.
- [ ] J'ai sauvé une baseline et vu une régression sortir non-zéro.
- [ ] Je sais énoncer le modèle de menace : public ⇒ relatif ; hébergé ⇒ chiffrement ≠ privé.

---

## 🎓 À propos de cette leçon

Ceci referme l'arc « confiance & vérification » du cours. La Leçon 11 a attrapé un
substrat se dégradant en silence (un canary pour les embeddings) ; la Leçon 15 a attrapé
un *relecteur* qui mentait (falsification manuelle) ; la Leçon 16 automatise cette
vérification en un *canary pour le modèle lui-même*. C'est aussi le pont vers
l'Itération 4 (l'apprentissage) : avant de laisser un agent *apprendre* d'un modèle — en
consolidant ses sorties dans la mémoire long terme — mieux vaut mesurer si ce modèle est
fiable, sinon tu cuis ses hallucinations dans les fondations. Mesure d'abord ; apprends
ensuite.

Retour à l'[index du cours](../).
