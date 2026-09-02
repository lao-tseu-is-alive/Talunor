# Lesson 06 — worked solution · corrigé de la leçon 06

**🇬🇧 English**

This branch holds the worked solution to
[Lesson 06 — Build your first tool](docs/lessons/06-build-your-first-tool/README.md):

```text
internal/tools/unitconvert.go       the tool
internal/tools/unitconvert_test.go  the table tests
cmd/talunor/main.go                 the one line of composition
```

**It is not on `main`, and it never will be.** On `main` the answer would sit next
to the question: every reader would open `internal/tools/` and find the exercise
already done. `docs/lessons/assertions.sh` enforces that — the assertion
`06 unit_convert is still NOT implemented upstream` fails `make release-check` the
moment `internal/tools/unitconvert.go` appears there.

Which means, on purpose:

- **`make release-check` FAILS on this branch**, on that assertion (and on
  `atlas-check`, since these files are not in `docs/atlas.md`). That is the guard
  doing its job, not a defect. `make test` passes.
- **Do not merge this branch.** It exists to be read, not to be integrated.

## How to use it

Try the exercise first — the discovery is the lesson, and the prediction step
(what does `{"from":"c"}` return?) only works once. Then compare:

```bash
git diff learning/unit-convert-tool solutions/06-unit-convert -- internal/tools/
```

Your version does not have to match this one. Two things are worth checking:

1. `Value` is a `*float64`, and `From` is not a pointer — reach for a pointer where
   the zero value is a legal input, not by reflex;
2. your table has **both** `0 c → 32 f` and a missing `value` that errors. Either
   case alone proves nothing: a plain `float64` field passes the first and fails
   the second.

Branched from `v0.23.10`. It may lag `main`; the lesson is the reference, this is
one way of satisfying it.

---

**🇫🇷 Français**

Cette branche contient le corrigé de la
[Leçon 06 — Construire ton premier outil](docs/lessons/06-build-your-first-tool/README.fr.md).

**Elle n'est pas sur `main`, et elle n'y sera jamais.** Sur `main`, la réponse
serait à côté de la question : chaque lecteur ouvrirait `internal/tools/` et
trouverait l'exercice déjà fait. `docs/lessons/assertions.sh` l'impose —
l'assertion `06 unit_convert is still NOT implemented upstream` fait échouer
`make release-check` dès que le fichier apparaît là-bas.

Ce qui veut dire, délibérément :

- **`make release-check` ÉCHOUE sur cette branche**, sur cette assertion (et sur
  `atlas-check`, ces fichiers n'étant pas dans `docs/atlas.md`). C'est la garde qui
  fait son travail, pas un défaut. `make test` passe.
- **Ne fusionne pas cette branche.** Elle est faite pour être lue, pas intégrée.

## Comment s'en servir

Fais d'abord l'exercice — la découverte *est* la leçon, et l'étape de prédiction
(que renvoie `{"from":"c"}` ?) ne fonctionne qu'une fois. Ensuite compare :

```bash
git diff learning/unit-convert-tool solutions/06-unit-convert -- internal/tools/
```

Ta version n'a pas à être identique. Deux choses méritent d'être vérifiées :

1. `Value` est un `*float64` et `From` n'est pas un pointeur — on prend un pointeur
   là où la zero value est une entrée légale, pas par réflexe ;
2. ta table contient **à la fois** `0 c → 32 f` et une `value` absente qui échoue.
   Aucun des deux cas ne prouve quoi que ce soit isolément : un champ `float64`
   ordinaire passe le premier et rate le second.

Branchée sur `v0.23.10`. Elle peut retarder sur `main` ; la leçon est la référence,
ceci n'est qu'une manière de la satisfaire.
