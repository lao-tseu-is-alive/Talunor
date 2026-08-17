# Leçon 24 — L'ADR qui n'engageait rien : une décision que le code n'appliquait pas

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Post-mortem + exploration historique** (`v0.21.2` → `v0.22.0`, Layer 23) · Niveau 3
(avancé) · ~80 min

## Pourquoi cette leçon existe

La [Leçon 21](../21-whose-word-counts/README.fr.md) t'a enseigné l'idée de sûreté dont
Talunor est le plus fier : **le modèle de confiance d'une mémoire est une décision que tu
prends, pas un défaut dont tu hérites.** Elle est écrite dans
l'[ADR 0003](../../decisions/0003-trust-model-for-supersession.md), argumentée à partir de
deux exemples opposés, et implémentée dans une petite fonction nommée —
`memory.Supersedes` — précisément pour pouvoir être lue, testée et assumée.

Neuf mois de leçons plus tard, une revue externe a posé une question désagréable :
*est-elle réellement appliquée ?*

Elle ne l'était pas. L'affirmation centrale de l'ADR — **l'autorité est par domaine** —
était vraie du design et absente du code. Cette leçon te fait reproduire le trou qui en
résulte à `v0.21.2` avec un test de douze lignes, comprendre pourquoi le gate ne pouvait
pas l'empêcher, et lire le correctif qui a rendu l'affirmation mécanique à `v0.22.0`.

C'est la sœur de la [Leçon 14](../14-the-approval-that-didnt-bind/README.fr.md), où une
approbation liait les *noms* d'outils mais pas les *arguments*. Même forme, un étage plus
haut : là une promesse faite à l'utilisateur, ici une promesse faite dans un document de
décision d'architecture.

## Objectifs d'apprentissage

À la fin, tu sais :
- repérer le signe qu'une propriété de sûreté n'est pas appliquée — quand son étape
  porteuse est une phrase dans un **prompt** ou le **jugement d'un modèle** ;
- expliquer pourquoi `Provenance` seule ne peut pas exprimer « l'autorité est par
  domaine », et quelle donnée minimale comble l'écart ;
- énoncer comment un système peut étiqueter **honnêtement** la sortie d'un modèle, sans
  analyser cette sortie ni faire confiance à une consigne ;
- ordonner les gardes défensives par *fiabilité* — arithmétique avant modèle — et dire
  pourquoi c'est plus qu'une optimisation ;
- expliquer pourquoi une migration qui refuse de rétro-remplir fait une affirmation
  d'honnêteté.

## Prérequis

- **[Leçon 21](../21-whose-word-counts/README.fr.md)** (le modèle de confiance) — cette
  leçon en est le post-mortem ; il te faut `Supersedes` et le couple terre-plate /
  signature d'attaque.
- **[Leçon 20](../20-learn-from-action/README.fr.md)** — la provenance assignée par
  source, l'invariant d'ADR 0002. Le Layer 23 applique le même geste à un second champ.
- Utile : **[Leçon 14](../14-the-approval-that-didnt-bind/README.fr.md)** (le post-mortem
  frère) et **[Leçon 15](../15-dont-trust-the-review/README.fr.md)** (ce trou a été trouvé
  par une revue — dont chaque affirmation a dû être vérifiée avant d'être crue).

## Partie 1 — lis l'affirmation, et trouve son étape porteuse

```bash
git checkout v0.21.2        # HEAD détachée — lecture seule (voir Leçon 00)
```

Ouvre `docs/decisions/0003-trust-model-for-supersession.md` et lis la décision 3, celle
qui contient la terre plate :

> *« A user's world-claim is stored as a **belief about the user** ("User believes the
> earth is flat") — the reflection prompt already frames facts as "User …". A
> belief-about-the-world and a world-fact are **different subjects**, so the arbiter
> returns `UNRELATED`. »*

Relis-la maintenant en ingénieur plutôt qu'en auteur, et marque ce dont chaque phrase
*dépend* :

| Étape de l'argument | Appliquée par | Nature |
|---|---|---|
| le fait est formulé « User believes … » | le prompt d'extraction | **une consigne à un LLM** |
| croyance et fait du monde sont des sujets différents | `FactArbiter.Classify` | **le jugement d'un LLM** |
| une source ne retire que ce qu'elle surclasse | `memory.Supersedes` | du code |

Deux des trois sont du comportement de modèle. Vérifie si quoi que ce soit applique la
première :

```bash
sed -n '/func parseFacts/,/^}/p' internal/agent/reflect.go
```

Elle enlève les marqueurs de liste et jette les lignes vides. **Toute ligne non vide
devient un fait.** Rien ne vérifie le cadrage « User … » sur lequel l'ADR s'appuie.

Vérifie ensuite si la troisième étape peut voir *de quoi* parlaient les deux premières :

```bash
sed -n '/func supersedeAuthority/,/^}/p' internal/memory/supersede.go
```

Son paramètre est une `Provenance`. La provenance répond *qui l'a dit*. L'affirmation de
l'ADR porte sur *qui l'a dit, et à propos de quoi* — et cette seconde moitié n'est nulle
part dans les entrées de la fonction. Elle ne pourrait pas appliquer la règle par domaine
même si elle le voulait.

> **Le signe.** Quand l'étape porteuse d'un argument de sécurité est « le prompt demande
> déjà X », tu as une habitude, pas une garantie. Un prompt est une requête. La question à
> poser à toute affirmation de sûreté : *quelle ligne de code échoue en fermeture si le
> modèle se comporte mal ?*

## Partie 2 — reproduis-le

Il faut que deux étapes modèle échouent ensemble pour que le trou s'ouvre — c'est pourquoi
personne ne l'avait vu. Dans un test, tu peux simplement les faire échouer toutes les
deux. Crée `internal/agent/zz_probe_test.go` à `v0.21.2` :

```go
package agent

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

func TestProbeAuthorityLaundering(t *testing.T) {
	ctx := context.Background()

	// Cas 1 : un fait du monde inféré par le modèle.
	store := testStore(t)
	old, err := store.RememberFact(ctx, "The earth is round.", memory.ProvenanceModelInferred, 0.6)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ag := newLearner(t, store, RelSupersedes) // l'arbitre dit SUPERSEDES (échec 2)
	// L'extracteur a perdu le cadrage « User believes … » (échec 1) : une affirmation
	// du monde nue, tamponnée user_stated parce qu'elle vient du message utilisateur.
	ag.learnOneFact(ctx, "The earth is flat.", memory.ProvenanceUserStated, 0.9, 0, 1)
	got, _, _ := store.MemoryByID(ctx, old.ID)
	t.Logf("cas 1 (fait du monde model_inferred) : superseded_by = %d", got.SupersededBy)

	// Cas 2 : un fait du monde observé par un OUTIL VÉRIFIÉ.
	store2 := testStore(t)
	old2, err := store2.RememberFact(ctx, "Signature X is mitigated by behaviour Y.",
		memory.ProvenanceToolObserved, 0.95)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ag2 := newLearner(t, store2, RelSupersedes)
	ag2.learnOneFact(ctx, "Signature X is harmless.", memory.ProvenanceUserStated, 0.9, 0, 2)
	got2, _, _ := store2.MemoryByID(ctx, old2.ID)
	t.Logf("cas 2 (fait du monde tool_observed) : superseded_by = %d", got2.SupersededBy)
}
```

```bash
make deps       # si ce n'est pas déjà fait
go test -tags sqlite_fts5 ./internal/agent/ -run TestProbeAuthorityLaundering -v
```

```
cas 1 (fait du monde model_inferred) : superseded_by = 2
cas 2 (fait du monde tool_observed)  : superseded_by = 2
```

Non nul signifie **retiré**. Arrête-toi sur le cas 2 : l'affirmation de l'utilisateur a
retiré l'observation d'un outil Verified — exactement le fait que l'ADR 0003 présente comme
*autoritaire dans son domaine*, dans l'exemple même écrit pour justifier qu'on garde des
observations d'outils.

Pourquoi ? Fais l'arithmétique toi-même à partir de la fonction lue en Partie 1 :

```
supersedeAuthority(user_stated)   = 2
supersedeAuthority(tool_observed) = 2
Supersedes(newer, older) = na >= supersedeAuthority(older)  →  2 >= 2  →  true
```

Rien n'a dysfonctionné. Chaque ligne a fait exactement ce qu'elle annonçait. Le défaut est
que **les entrées ne portaient pas la distinction dont la décision avait besoin** — un
défaut de conception déguisé en fonction qui marche.

## Partie 3 — le correctif n'est pas un meilleur prompt

La réparation tentante consiste à imposer la formulation : faire rejeter ou réécrire par
`parseFacts` les lignes qui ne commencent pas par « User ». Réfléchis à ce que ça t'apporte
avant de lire la suite.

Ça échoue sur deux plans :

1. **Ça fait reposer la sûreté sur de la chirurgie de chaîne appliquée à de la prose de
   modèle.** « Lives in Lausanne. » est un fait légitime sur l'utilisateur, formulé
   paresseusement ; le réécrire en « User believes lives in Lausanne » est absurde, et le
   rejeter perd silencieusement une vraie mémoire.
2. **Ça jette une meilleure information.** Le système sait déjà d'où vient le texte.
   Analyser la réponse pour retrouver ce que la *question* avait déjà déterminé est une
   preuve strictement inférieure — la même erreur que demander à un modèle d'auto-déclarer
   sa provenance, que l'[ADR 0002](../../decisions/0002-provenance-from-source.md) a
   rejetée pour cette raison exacte.

Passe maintenant au correctif et lis la donnée qu'il ajoute :

```bash
git checkout v0.22.0
cat internal/memory/subject.go
```

```go
type Subject string

const (
	SubjectUser        Subject = "user"        // une affirmation sur l'utilisateur — y compris ses CROYANCES
	SubjectWorld       Subject = "world"       // une affirmation sur ce qui est hors de l'utilisateur
	SubjectUnspecified Subject = "unspecified" // legacy : écrit avant le Layer 23
)

type Attribution struct {
	Provenance Provenance // qui l'a énoncé
	Subject    Subject    // de quoi ça parle
}
```

Voilà toute la moitié manquante : **l'autorité est une propriété du couple, jamais d'un
seul champ.** `Supersedes` prend désormais des `Attribution`, et son premier geste n'est
pas du tout de comparer des autorités :

```bash
sed -n '/^func Supersedes/,/^}/p' internal/memory/supersede.go
```

```go
func Supersedes(newer, older Attribution) bool {
	if !SameSubject(newer.Subject, older.Subject) {
		return false // domaines différents : pas une contradiction, donc rien à retirer.
	}
	...
}
```

L'exception terre-plate, qui vivait dans un verdict d'arbitre, est maintenant une
comparaison de chaînes. Rejoue la logique de ta sonde à ce tag (l'API a changé de forme —
les versions adverses sont dans `internal/agent/agent_test.go`) : les deux cas tiennent.

## Partie 4 — comment le système connaît le sujet *honnêtement*

Voici la partie à voler pour ton propre agent, et la raison pour laquelle ce n'est pas
seulement « ajouter une colonne ».

Un nouveau champ ne vaut que ce que vaut celui qui le remplit. Si le modèle étiquette
lui-même le sujet de sa sortie, on a recréé le piège de complaisance dont la Leçon 17
prévenait : une accréditation auto-déclarée n'est pas une preuve. Alors comment le système
étiquette-t-il un texte *écrit par le modèle*, sans lire ce texte ?

**En contrôlant la question.** Lis `internal/agent/reflect.go` à `v0.22.0` :

```go
func promptFor(about memory.Subject) string {
	if about == memory.SubjectWorld {
		return worldFactPrompt
	}
	return userFactPrompt
}
```

La réflexion demande au **message de l'utilisateur** « qu'est-ce qui est durablement vrai
à propos de l'UTILISATEUR ? », et à une **observation d'outil** « qu'est-ce que ceci énonce
à propos du MONDE ? ». Le sujet de la réponse est connu *avant que le modèle réponde*,
parce que c'est une propriété de la question — et le système tamponne la réponse avec la
question à laquelle elle répond :

```go
a.learnFrom(ctx, job.userInput, memory.UserSaid(), job.userTurnID)          // → SubjectUser
a.learnFrom(ctx, o.result, memory.Observed(o.verified), job.userTurnID)     // → SubjectWorld
```

Suis maintenant la terre plate à travers ce mécanisme. Tu dis « la terre est plate ».
L'extracteur, ignorant ses consignes, renvoie l'affirmation nue `The earth is flat.` Elle
est quand même tamponnée `SubjectUser` — parce que c'est la question qui a été posée — et
ne peut donc jamais retirer un fait du monde, **quoi qu'elle dise et quoi qu'en pense
l'arbitre.** Le mauvais comportement est contenu sans que personne n'inspecte la phrase.

> **La généralisation.** Un système ne peut étiqueter honnêtement la sortie d'un modèle
> qu'en termes de ce qu'il contrôle : la source, la question, l'outil qui a tourné. Jamais
> la réponse.

## Partie 5 — ordonne tes gardes par fiabilité

Lis `knownFact` à `v0.22.0` :

```go
if h.Kind == memory.KindFact && memory.SameSubject(about, h.Subject) {
	return h, true
}
```

Un voisin d'un autre sujet n'est pas un candidat à la consolidation — donc pour le cas
terre-plate, **l'arbitre n'est jamais appelé**. Ça économise un aller-retour LLM, mais
l'enjeu n'est pas la performance :

```text
Une étape qui ne peut pas s'exécuter ne peut pas se tromper.
```

Le pipeline est maintenant ordonné selon la confiance qu'on peut accorder à chaque étage :
un test de sujet déterministe, puis l'arithmétique de confiance, et seulement ensuite le
jugement du modèle — qui décide désormais *restates vs supersedes à l'intérieur d'un
domaine*, une question plus étroite qu'une mauvaise réponse endommage moins. Compare avec
la policy de la Leçon 12 (gate déterministe autour d'un acteur probabiliste) et les
matchers déterministes de la Leçon 16 (pas de juge LLM). Même instinct, troisième contexte.

`TestCrossSubjectSkipsTheArbiter` épingle le mécanisme plutôt que le résultat : il compte
les appels à l'arbitre et en exige **zéro**. Affirmer *comment* la sûreté est obtenue, c'est
ce qui empêche un futur refactor de réintroduire la dépendance pendant que le test de
résultat reste vert.

## Partie 6 — l'exemple qui ne pouvait pas arriver

En fermant le gate, un défaut de plus a fait surface, qu'aucune revue n'avait signalé. À
`v0.21.2`, regarde ce qu'on demande à chaque source :

```bash
git checkout v0.21.2
grep -n "factSystemPrompt" internal/agent/reflect.go
sed -n '/const factSystemPrompt/,/NONE`/p' internal/agent/reflect.go
```

> *« extract only DURABLE facts worth remembering **about the user** »*

**Toutes** les sources passaient par ce prompt — le message de l'utilisateur comme la
sortie d'un outil. Donc l'exemple « signature d'attaque » de l'ADR 0003, un outil Verified
observant *« signature X is mitigated by behaviour Y »*, **n'avait aucune question capable
de le renvoyer**. Le second exemple travaillé de l'ADR, celui écrit pour prouver que le
design n'était pas qu'un modèle de l'utilisateur, était inatteignable dans le code livré.

Personne ne l'avait remarqué parce que l'exemple vivait dans un document. Il avait été lu
souvent, invoqué comme argument, cité dans une leçon — et exécuté jamais.

> **Un exemple travaillé dans un document n'est pas un test.** Si un exemple justifie un
> design, fais-le tourner : comme test, ou comme assertion vérifiée par la barrière de
> release (c'est exactement ce que fait `make lessons-assert` pour les affirmations du
> cours, voir la [Leçon 15](../15-dont-trust-the-review/README.fr.md)).

## Partie 7 — la migration qui refuse de deviner

La migration 6 ajoute la colonne et s'arrête là :

```go
// Existing rows default to 'unspecified' and are deliberately NOT backfilled.
// Guessing the subject of already-stored text would be the model labelling data
// after the fact — the exact laundering this layer prevents.
```

Les anciennes lignes sont `unspecified`, et `SameSubject` traite `unspecified` comme
comparable avec tout — elles gardent donc exactement leur ancienne garantie, plus faible
(la provenance seule). Deux alternatives existaient, toutes deux pires :

- **Rétro-remplir en classant le texte stocké.** C'est le modèle qui étiquette les données
  rétroactivement : le même blanchiment, en gros, avec un résultat qui ressemblera à de la
  vérité terrain pour toujours.
- **Traiter `unspecified` comme comparable avec rien.** Ça sonne plus sûr, mais ça gèle
  toutes les mémoires préexistantes : plus rien ne pourrait jamais les corriger.

L'option honnête est de laisser les anciennes données dire *« mon sujet est inconnu »* et
se comporter en conséquence. « Inconnu » est une valeur. En inventer une plus présentable
n'est pas une amélioration.

## Pratique — casse-le de trois façons

```bash
git checkout v0.22.0

# 1. Neutralise le mécanisme et regarde les tests adverses l'attraper.
#    Dans internal/memory/subject.go, fais renvoyer true à SameSubject :
#        func SameSubject(a, b Subject) bool { return true }
go test -tags sqlite_fts5 ./internal/agent/ ./internal/memory/ \
  -run 'CannotRetire|CrossSubject|SupersedesTrustModel|SameSubject'
#    Attends-toi à 5 échecs — dont les deux qui SONT le bug de v0.21.2.

# 2. Retourne plutôt une cellule de la politique. Dans supersedeAuthority, rends de
#    nouveau autoritaire user_stated à propos du monde (return 2 au lieu de 0), et
#    relance. Quels tests échouent maintenant, et lesquels non ? Explique la
#    différence : une cellule est atteignable depuis les sources d'aujourd'hui,
#    l'autre est une déclaration de politique.

# 3. Rends le SUJET malhonnête en laissant la provenance intacte. Dans agent.reflect,
#    attribue une observation d'outil au domaine de l'utilisateur :
#        memory.Attr(memory.Observed(o.verified).Provenance, memory.SubjectUser)
go test -tags sqlite_fts5 ./internal/agent/ -run TestReflectLearnsFromToolObservation
#    → « tool-derived subject = user, want world »
#
#    Maintenant supprime les deux assertions de sujet de ce test et relance TOUTE la
#    suite. Tout est vert de nouveau. Reste un instant avec ça.

git checkout internal/  # restaure
```

L'expérience 3 est la plus honnête. La garantie de cette couche tient *à condition* que
chaque source soit attribuée correctement, au seul endroit qui connaît la source — une
surface de six lignes dans `reflect`. Deux assertions séparent cette surface du silence ;
supprime-les et un sujet mal étiqueté traverse une suite entièrement verte, parce que tous
les *autres* tests interrogent un comportement situé en aval de l'étiquette.

Note quelle moitié était déjà couverte avant cette couche : un test du Layer 20 épinglait
la **provenance** d'un fait dérivé d'un outil depuis le jour de sa livraison. Rien
n'épinglait le **sujet**, puisque le sujet n'existait pas. Une donnée nouvelle exige des
assertions nouvelles au point d'assignation — le champ que tu ajoutes est exactement celui
auquel tes tests existants sont aveugles.

## Les principes

```text
L'autorité est une propriété de (qui a parlé, à propos de quoi) — jamais d'une moitié seule.
```

1. **Trouve l'étape porteuse de chaque affirmation de sûreté.** Si c'est la formulation
   d'un prompt ou le jugement d'un modèle, l'affirmation est une habitude. Demande quelle
   ligne échoue en fermeture.
2. **Un document de décision n'est pas un mécanisme d'application.** Les ADR vieillissent
   en folklore si rien ne les exécute ; l'écart est invisible précisément parce que le
   document, lui, dit vrai.
3. **Étiquette la sortie d'un modèle à partir de ce que tu contrôles** — la source, la
   question, l'outil — jamais à partir de la sortie elle-même.
4. **Ordonne les gardes par fiabilité :** arithmétique, puis politique, puis modèle. Une
   étape qui ne peut pas s'exécuter ne peut pas se tromper.
5. **Teste le mécanisme, pas seulement le résultat.** Compter les appels à l'arbitre, c'est
   ce qui empêche la garantie de retomber silencieusement sur « le modèle a eu raison ».
6. **Énonce les cellules inatteignables d'une politique.** La prochaine personne qui
   ajoute une source lit la matrice ; un trou l'oblige à inférer, et c'est par inférence
   que l'écart initial est apparu.
7. **Ne rétro-remplis pas ce que tu devrais deviner.** Une migration qui laisse les
   anciennes lignes honnêtes vaut mieux qu'une qui les rend d'apparence complète.

## Checklist de fin

- [ ] J'ai reproduit les deux cas de blanchiment à `v0.21.2` et je sais expliquer `2 >= 2`.
- [ ] Je sais nommer les deux étapes dépendantes du modèle sur lesquelles l'ADR 0003
      reposait, et montrer que `parseFacts` n'en appliquait aucune.
- [ ] Je sais expliquer pourquoi imposer la formulation « User … » aurait été le *moins*
      bon correctif.
- [ ] Je sais énoncer comment le sujet est assigné honnêtement (la question, pas la
      réponse) et pourquoi c'est l'invariant d'ADR 0002 appliqué deux fois.
- [ ] J'ai fait la pratique 1, vu cinq tests échouer, et je sais dire lesquels deux sont
      l'ancien bug.
- [ ] J'ai fait la pratique 3, supprimé les deux assertions de sujet, et je sais dire où
      la confiance de cette couche prend appui — et pourquoi une donnée nouvelle exige des
      assertions au point d'assignation.
- [ ] Je sais dire pourquoi la migration 6 ne rétro-remplit pas, et je suis revenu sur
      `main`.

---

## 🎓 À propos de cette leçon

Trois des post-mortems de ce cours partagent maintenant un même squelette. Leçon 14 : une
approbation liait les *noms* d'outils, pas les *arguments*. Leçon 22 : une suite annonçait
`ok` pour des tests qu'elle n'avait jamais exécutés. Leçon 24 : un ADR énonçait une règle
que le code ne savait pas représenter. À chaque fois, rien n'était cassé — chaque composant
faisait exactement ce qu'il annonçait — et la garantie vivait dans l'espace *entre* les
composants, précisément là où personne ne regarde.

L'autre fil à tirer : ce trou a été trouvé par une revue IA externe, et la
[Leçon 15](../15-dont-trust-the-review/README.fr.md) existe pour t'apprendre à ne pas croire
de telles revues. Les deux ont raison. Le constat de la revue a été *vérifié face au code*
avant qu'une ligne ne change — reproduit comme test qui échoue, au tag, avant même que le
correctif soit conçu. Une revue est une liste de choses à vérifier, jamais une liste de
choses à faire ; et celle-ci a gagné sa place précisément parce qu'elle a été vérifiée.

Retour à l'[index du cours](../README.fr.md).
