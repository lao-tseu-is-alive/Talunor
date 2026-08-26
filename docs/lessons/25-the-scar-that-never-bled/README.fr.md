# Leçon 25 — La cicatrice qui n'a jamais saigné : concevoir une décision qui lie

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique + étude de conception** (`v0.22.5` → `v0.23.0`, couche 24) ·
Niveau 3 (avancé) · ~75 min

## Pourquoi cette leçon existe

Quatre leçons de ce cours sont des post-mortems — [11](../11-when-memory-forgets/),
[14](../14-the-approval-that-didnt-bind/), [22](../22-the-silent-suite/),
[24](../24-the-adr-that-didnt-bind/). Chacune part de quelque chose qui a cassé, et
chacune tire son autorité de la cicatrice : *on sait que ça compte parce que ça nous a
coûté.*

Celle-ci est différente, et la différence est la leçon.

De `v0.20.0` à `v0.23.0` — **quatre couches, plusieurs semaines** — la mémoire de Talunor
détruisait de l'information chaque fois que son modèle de confiance refusait une
correction. Rien ne plantait. Aucun test n'échouait. Aucun utilisateur ne se plaignait. Le
défaut était **fail-open sur la connaissance** : le système ressemblait exactement à un
système qui fonctionne, parce que refuser une mauvaise correction *est* le bon
comportement, et l'oubli était invisible.

Ce n'est donc pas une leçon écrite avant la cicatrice. C'est **la première leçon sur une
cicatrice qui n'a jamais saigné** — un défaut trouvé par *lecture* et non par incident,
dans du code dont les tests sont restés verts tout du long. Cette classe de défaut mérite
son propre traitement, car aucune des techniques des quatre autres post-mortems ne
l'aurait trouvée : pas de test rouge à bissecter, pas d'erreur à tracer, pas de
commentaire de revue à falsifier.

Puis elle fait ce que le cours n'a pas encore fait : elle prend **l'instrument construit
en leçon 24** — *marquer chaque phrase d'un ADR selon ce qui la rend vraie* — et le
retourne sur un ADR écrit après, [ADR 0005](../../decisions/0005-contested-claims.md).
Cette fois l'instrument revient vert, phrase par phrase, et c'est toi qui le vérifies
plutôt que de croire quiconque sur parole.

> **Un avertissement sur le genre même de cette leçon.** « Voici une décision qu'on a
> bien prise » est le type de documentation le plus facile à écrire mal. La défense, c'est
> qu'on te donne l'outil et les preuves, et que tu les exécutes. Si l'ADR 0005 échoue au
> test de la partie 4 quand tu l'appliques, la leçon a tort et tu dois le dire — c'est
> exactement ce à quoi la [leçon 15](../15-dont-trust-the-review/) t'a entraîné.

## Objectifs d'apprentissage

À la fin, tu sais :

1. Décrire le **fail-open sur la connaissance** et expliquer pourquoi il est plus dur à
   détecter qu'un plantage, une mauvaise réponse ou un test rouge.
2. Reproduire, à `v0.22.5`, une gate qui refuse correctement et oublie silencieusement.
3. Appliquer l'instrument de la leçon 24 — *qu'est-ce qui rend cette phrase vraie ?* — à
   une conception proposée, et t'en servir pour rejeter une machine à états d'apparence
   rigoureuse.
4. Expliquer pourquoi **dériver** un statut dissout la question « qui déplace le jeton ? »
   au lieu d'y répondre, et le relier au lazy decay de la couche 17.
5. Reconnaître la **porte dérobée de l'autorité** : re-litiger par l'arithmétique une
   décision tranchée par une gate.
6. Casser la dérivation délibérément, fabriquer la dérive qu'elle empêche, et constater
   que rien ne la détecte.

## Prérequis

- [Leçon 21](../21-whose-word-counts/) — le modèle de confiance et `memory.Supersedes`.
- [Leçon 24](../24-the-adr-that-didnt-bind/) — **obligatoire** : cette leçon utilise son
  instrument comme outil et suppose que tu t'en es servi une fois.
- Utile : [leçon 18](../18-the-memory-of-the-gesture/) pour le lazy decay, et
  [leçon 20](../20-learn-from-action/) pour la trace d'évidence.

## Partie 1 — la forme d'un défaut invisible

Lis le point de rejet tel qu'il a tenu pendant quatre couches :

```bash
git checkout v0.22.5        # HEAD détachée — lecture seule (voir leçon 00)
sed -n '/case RelSupersedes:/,/case RelUnrelated:/p' internal/agent/learn.go
```

Tu trouveras, dans la branche `else` :

```go
} else {
    // The trust model forbids it — the old belief is more authoritative than
    // this source. Drop the new fact rather than store a contradiction.
    a.trace("supersede.denied", ...)
}
```

Arrête-toi sur le commentaire. Il est **honnête, exact, et décrit un choix délibéré.**
Personne n'a été négligent ici : « laisser tomber le nouveau fait plutôt que stocker une
contradiction » est une phrase que quelqu'un a écrite exprès.

Liste maintenant ce que le système sait à cet instant, et ce qui survit :

| Connu au moment du refus | Survit à la branche `else` |
|---|---|
| Deux sources sont en désaccord sur le même sujet | ✗ |
| Laquelle manquait d'autorité pour l'emporter | ✗ |
| Le texte exact de l'affirmation perdante | ✗ |
| Le tour où elle est arrivée | ✗ |
| Que la question ait seulement été soulevée | seulement si `TALUNOR_DEBUG` est actif |

Tout est jeté. Et voici pourquoi aucun test ne l'a attrapé : **il n'y avait rien à
asserter.** Un test peut vérifier que le refus a eu lieu (`superseded_by == 0` — et il en
existait un, depuis la couche 23). Aucun test ne peut naturellement vérifier qu'une chose
absente aurait dû être présente, à moins que quelqu'un ait déjà eu l'idée qu'elle devrait
l'être.

> **Fail-closed sur l'autorité, fail-open sur la connaissance.** La gate a correctement
> refusé qu'une source faible écrase une source forte. Elle a aussi, silencieusement, rendu
> le système incapable de se souvenir que quelqu'un avait essayé. La première moitié est un
> garde-fou ; la seconde est une perte de données déguisée en garde-fou.

## Partie 2 — reproduis-le

Toujours à `v0.22.5`. Ajoute cette sonde dans `internal/agent/` :

```go
// probe_test.go — à v0.22.5. À supprimer ensuite.
func TestProbeRefusalIsSilent(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	observed, _ := store.RememberFact(ctx, "Signature X is mitigated by behaviour Y.",
		memory.Observed(true), 0.95)          // tool_observed, monde → autorité 2
	ag := newLearner(t, store, RelSupersedes) // l'arbitre propose la supersession

	ag.learnOneFact(ctx, "Signature X is not mitigated at all.",
		memory.Observed(false), 0.5, 0, 42)   // model_inferred → autorité 0

	got, _, _ := store.MemoryByID(ctx, observed.ID)
	t.Logf("superseded_by = %d  (0 = le refus a tenu — correct)", got.SupersededBy)

	ev, _ := store.EvidenceFor(ctx, observed.ID)
	t.Logf("lignes d'évidence = %d  (que reste-t-il du défi ?)", len(ev))
}
```

```bash
go test -tags sqlite_fts5 ./internal/agent/ -run TestProbeRefusalIsSilent -v
```

Attendu :

```text
superseded_by = 0  (0 = le refus a tenu — correct)
lignes d'évidence = 0  (que reste-t-il du défi ?)
```

Les deux lignes comptent. La première dit que le garde-fou a marché. La seconde dit que
l'événement n'a laissé aucune trace atteignable par un utilisateur ou un auditeur. **Un
système qui fonctionne et un système amnésique donnent ici la même observation** — ce qui
explique précisément que quatre couches soient passées.

Supprime la sonde et reviens sur `main` :

```bash
rm internal/agent/probe_test.go
git checkout main
```

## Partie 3 — la conception tentante, et l'instrument qui la rejette

Le manque est maintenant évident, et la note de recherche de Talunor décrivait déjà la
forme visée. Lis-la :

```bash
sed -n '/^Unexamined/,/^Superseded/p' docs/epistemic-reasoning-vision.md
```

```text
Unexamined
 -> Hypothesis
 -> Supported
 -> StronglySupported
 -> Established

with possible branches:

Contested
Refuted
Superseded
```

Ça a l'air rigoureux. Il y a des états, des transitions, et un vocabulaire emprunté à
l'épistémologie. Il serait tout à fait raisonnable de l'implémenter.

**Applique l'instrument de la leçon 24 avant de le faire.** Pour chaque transition, écris
ce qui la déciderait :

| Transition | Qu'est-ce qui déplace le jeton ? |
|---|---|
| `Unexamined → Hypothesis` | ? |
| `Hypothesis → Supported` | ? |
| `Supported → StronglySupported` | ? |
| `StronglySupported → Established` | ? |

Fais-le honnêtement avant de lire la suite.

Chaque réponse est une variante de *« quand il y a assez d'évidence »* — et **« assez »
n'a aucune définition dans le code.** Ce qui signifie, en pratique, que le modèle décide :
on demande à un LLM si l'évidence suffit désormais, et sa réponse déplace le jeton.

C'est le piège de l'[ADR 0002](../../decisions/0002-provenance-from-source.md) avec des
étapes en plus. La couche 16 a refusé qu'un modèle rapporte sa propre confiance ; une
machine à états dont les transitions sont des jugements du modèle est la même chose
déguisée en diagramme. C'est aussi **exactement l'erreur dont parle la leçon 24** — un ADR
dont le mécanisme s'avère être un comportement du modèle — et cette fois elle était
disponible *à l'avance*, par écrit, avant la moindre ligne de code.

> **Le geste transférable :** une machine à états ne vaut que ses *fonctions* de
> transition. Les états sont décoratifs ; décider qui déplace le jeton est toute la
> conception. Si tu ne peux pas nommer une fonction déterministe pour une transition, tu
> as dessiné une décision, tu ne l'as pas prise.

## Partie 4 — retourne l'instrument sur l'ADR 0005 elle-même

Lis maintenant la décision qui a été livrée :

```bash
sed -n '/^## Decision/,/^## Consequences/p' docs/decisions/0005-contested-claims.md
```

Prends ses six décisions numérotées et marque chacune comme tu as marqué la machine à
états. Pour chacune, demande : **qu'est-ce qui rend ceci vrai — du code, ou un
comportement ?** Puis vérifie ta réponse dans le dépôt. La thèse de cette leçon est que
les six reviennent *code* ; vérifie-la.

| # | Décision | Ce qui la rend vraie | Vérifie toi-même |
|---|---|---|---|
| 1 | Les lignes d'évidence portent une polarité | schéma | `sed -n '/version: 7/,/^\t},/p' internal/memory/migrate.go` |
| 2 | Le claim refusé n'est pas stocké comme mémoire | code + test | `TestRefusedSupersessionIsRecordedAsCounterEvidence` |
| 3 | `Contested` est dérivé, jamais stocké | SQL | `grep -n 'func contestedExpr' -A 6 internal/memory/memory.go` |
| 4 | Un fait contesté est toujours rappelé, marqué | code + test | `TestContestedFactIsStillRecalled` |
| 5 | La contre-évidence ne bouge pas la confiance | code + test | `TestRefusedSupersessionIsRecordedAsCounterEvidence` |
| 6 | `/why` montre les deux côtés | code + test | `sed -n '/func splitEvidence/,/^}/p' internal/agent/commands.go` |

Les trois décisions porteuses sont verrouillées par des assertions dans un seul test.
Lis-les, et remarque que chacune est formulée comme la *propriété*, pas comme
l'implémentation :

```bash
sed -n '/func TestRefusedSupersessionIsRecordedAsCounterEvidence/,/^}/p' \
  internal/agent/agent_test.go | grep -nE 't\.Error|t\.Fatal'
```

Tu dois voir, entre autres :

```text
the refused correction must leave the incumbent contested, not vanish     ← décision 3
a refused claim must not erode the fact                                   ← décision 5
the refused claim was stored as a fact; it must live only as evidence detail ← décision 2
```

Compare maintenant les deux tableaux que tu as construits. La colonne de la machine à
états dit *modèle, modèle, modèle, modèle*. Celle de l'ADR 0005 dit *schéma, code, SQL,
code, code, code*. **Même instrument, même après-midi, verdicts opposés** — et c'est ce
qui en fait une démonstration plutôt qu'une affirmation.

> Remarque le choix délibéré de ne citer aucun numéro de ligne ci-dessus. Une référence
> comme `agent_test.go:1450` pourrit dès que quelqu'un insère un test au-dessus, et ce
> cours a déjà livré un exercice périmé de cette façon (voir leçon 08). **Les noms et les
> recettes `sed`/`grep` survivent aux éditions ; un numéro de ligne est de la dérive avec
> deux-points.**

## Partie 5 — comment la dérivation dissout la question

Lis le mécanisme :

```bash
grep -n 'func contestedExpr' -A 6 internal/memory/memory.go
```

```go
func contestedExpr(alias string) string {
	return `EXISTS(SELECT 1 FROM evidence ev WHERE ev.fact_id = ` + alias +
		`.id AND ev.polarity = 'contradicts')`
}
```

C'est tout. `Contested` n'est pas une colonne, pas un champ que quelqu'un écrit, pas un
drapeau que quelqu'un lève. C'est une question posée à l'évidence au moment de la lecture.

Regarde ce que devient la question « qui déplace le jeton ? » : **personne ne le
déplace.** Il n'y a pas de transition à décider, parce qu'il n'y a pas d'état stocké à
faire transiter. Le statut *est* l'évidence, re-dérivé à chaque lecture.

Deux propriétés en découlent, et toutes deux comptent plus qu'il n'y paraît :

1. **Le drapeau ne peut pas dériver de sa justification**, parce qu'il n'a pas
   d'existence indépendante. Une colonne `contested` stockée pourrait contredire les
   lignes d'évidence censées l'expliquer — et rien dans le système ne le remarquerait,
   parce que rien ne les compare. Dériver rend cette classe de bug *irreprésentable*.
2. **Le chemin de lecture reste sans écriture**, ce que le store exige : il épingle
   `SetMaxOpenConns(1)` (leçon 03), donc un rappel qui écrirait en retour se
   sérialiserait contre lui-même.

Si ces deux points te semblent familiers, c'est normal :

```bash
grep -n 'LAZY' internal/memory/salience.go | head -3
```

La couche 17 a fait exactement ce compromis pour la rétention — la salience effective est
calculée à la lecture, jamais réécrite. **La couche 24 applique le même raisonnement à la
vérité.** L'une porte sur combien un souvenir compte, l'autre sur s'il est contesté ;
l'argument est identique, et le remarquer vaut plus que chacune des deux couches prise
isolément.

## Partie 6 — le refus qui semble le plus raisonnable

L'ADR 0005 rejette quatre alternatives. Trois sont faciles. Lis la quatrième :

```bash
sed -n '/Let counter-evidence lower/,/implicitly\./p' docs/decisions/0005-contested-claims.md
```

L'idée : *un fait contredit dix fois devrait inspirer un peu moins confiance qu'un fait
jamais contredit.* Ce n'est pas une pensée sotte. Ça ressemble à peser l'évidence, ce qu'un
système épistémiquement soigneux devrait faire.

Suis la mécanique. La source contredisante a atteint ce chemin de code **parce que le
modèle de confiance l'a refusée** — `memory.Supersedes` a renvoyé `false`, c'est-à-dire :
*cette source n'a pas assez d'autorité pour changer ce fait.* Si son affirmation abaisse
ensuite la confiance du fait, la source a quand même changé le fait, d'une plus petite
quantité, via un nombre que personne ne re-dérive.

> **La porte dérobée de l'autorité.** Une décision tranchée explicitement à une gate,
> rouverte implicitement par l'arithmétique. La gate est visible, nommée, testée et
> documentée dans un ADR ; le coefficient est un flottant dans une formule. Les deux
> changent ce que le système croit — un seul des deux peut être relu.

C'est le refus de la couche 16 un étage plus haut. Avant : le modèle ne peut pas rapporter
sa propre confiance. Maintenant : une source qui a perdu l'argument d'autorité ne peut pas
en gagner une version partielle. Même forme, et il vaut la peine de la reconnaître *comme
forme*, car elle reviendra partout où un système possède à la fois une gate et un score.

Le coût est réel et assumé : un fait contredit cinquante fois est exactement aussi contesté
qu'un fait contredit une fois. **Un bit, pas une échelle.** L'ADR 0005 soutient que corriger
cela demande l'*indépendance* des évidences — cinquante reformulations d'une seule page web
ne sont pas cinquante sources — et qu'un coefficient de confiance serait une mauvaise
réponse à la bonne question. Voir le §15 de la note de vision.

## Hands-on — casse-le de trois façons

### 1. Stocke le statut, puis fabrique la dérive

C'est l'exercice important : rends réelle la conception *opposée* et regarde-la échouer.

Le store n'exporte pas son `*sql.DB`, donc écris ceci comme un test **interne** —
`package memory`, le motif qu'utilise déjà `internal/memory/lexical_internal_test.go`,
qui est la façon dont un test atteint `s.db` :

```go
// internal/memory/contested_drift_test.go — package memory. À supprimer ensuite.
func TestProbeStoredFlagCanDrift(t *testing.T) {
	store := internalTestStore(t)      // le helper de lexical_internal_test.go
	ctx := context.Background()

	// La conception que l'ADR 0005 a rejetée : un drapeau stocké à côté de l'évidence.
	if _, err := store.db.ExecContext(ctx,
		`ALTER TABLE memories ADD COLUMN contested_flag INTEGER NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	fact, _ := store.RememberFact(ctx, "The earth is round.", Observed(true), 0.9)

	// Fais maintenant ce que fait un bug ordinaire : écris l'un, oublie l'autre.
	if _, err := store.db.ExecContext(ctx,
		`UPDATE memories SET contested_flag = 1 WHERE id = ?`, fact.ID); err != nil {
		t.Fatal(err)
	}

	var stored int
	store.db.QueryRowContext(ctx,
		`SELECT contested_flag FROM memories WHERE id = ?`, fact.ID).Scan(&stored)
	m, _, _ := store.MemoryByID(ctx, fact.ID)

	t.Logf("drapeau stocké = %d, Contested dérivé = %v", stored, m.Contested)
}
```

```bash
go test -tags sqlite_fts5 ./internal/memory/ -run TestProbeStoredFlagCanDrift -v
```

```text
drapeau stocké = 1, Contested dérivé = false
```

**Deux réponses à une seule question, et rien dans le système ne les compare.** Écris
l'assertion qui attraperait ça. Tu constateras qu'il faut inventer un contrôle de cohérence
qui doit lui-même être exécuté, planifié et maintenu — une troisième chose qui peut
pourrir. Ce contrôle est le coût permanent que la conception dérivée ne paie pas.

### 2. Supprime la moitié positive de l'assertion

Ouvre le garde-fou exécutable du cours :

```bash
grep -n 'lesson 25' -A 12 docs/lessons/assertions.sh
```

Il vérifie **deux** choses : qu'aucune migration n'ajoute de colonne `contested` stockée,
*et* que `contestedExpr` existe. Supprime le second contrôle, puis supprime entièrement la
fonctionnalité de la couche 24, et relance :

```bash
make lessons-assert
```

Ça passe. Une assertion d'absence seule ne distingue pas « correctement dérivé » de
« entièrement supprimé » — elle est satisfaite par un dépôt où la fonctionnalité n'a jamais
existé. **Une assertion sur ce qui manque a besoin d'une assertion sœur sur ce qui est
présent**, sinon elle garde l'ensemble vide.

Restaure les deux moitiés.

### 3. Rends le fait contesté silencieux à nouveau

Dans `internal/agent/turn.go`, retire le marqueur `CONTESTED` de `fencedMemories` et
lance :

```bash
go test -tags sqlite_fts5 ./internal/agent/ -run CounterEvidence
```

Ça échoue — délibérément. Enregistrer la contestation d'un fait sans jamais la montrer au
modèle rendrait toute la couche *décorative* : de l'information stockée, auditée, et jamais
utilisée. Demande-toi quels autres drapeaux, dans les systèmes où tu as travaillé, sont
dans cet état.

## Les limites, honnêtement

Une leçon qui ne ferait que louer la conception serait le mode d'échec contre lequel
celle-ci met en garde. Lis ce que l'ADR 0005 admet, et prends-le au sérieux — deux points
viennent d'une relecture *après* l'implémentation :

- **Un refus enregistré est permanent, et a été rendu sous UNE politique de confiance.**
  `memory.Supersedes` est délibérément remplaçable (l'ADR 0003 existe pour qu'un agent de
  sécurité ou de recherche puisse la remplacer). Mais les lignes de contre-évidence sont
  écrites au moment du refus : une trace accumulée sous une politique continue d'affirmer
  les verdicts de *cette* politique après son remplacement. Cela ne renverse pas la
  décision — un statut stocké serait tout aussi périmé *et* sujet à la dérive — mais c'est
  une vraie propriété. Si cela devient gênant, enregistre la politique décisionnaire sur la
  ligne ; n'abandonne pas la dérivation.
- **Le drapeau est structurellement couplé à la table `evidence`, pour toujours.** Puisque
  `Contested` *est* `EXISTS(une ligne contradicts)`, « contesté, puis résolu » ne peut pas
  s'exprimer sans un concept de rétractation que la trace n'a pas.
- **Donc une croyance peut être contestée mais jamais réhabilitée.** Cette asymétrie mérite
  qu'on s'y arrête avant de s'appuyer sur le drapeau : rien ne l'efface, quel que soit le
  nombre de fois où le fait en place est ensuite re-confirmé.
- **Un bit, pas une échelle**, comme l'expliquait la partie 6.

## Post-scriptum — le garde-fou qui a arrêté la livraison de cette leçon

Pendant la préparation de `v0.23.0`, `make atlas-check` a fait échouer le commit : l'ADR
0005 était suivi par git et absent de `docs/atlas.md`. La livraison n'a pas pu avancer
tant que le nouvel ADR n'était pas catalogué.

C'est une petite chose, et c'est la leçon en miniature. La livraison qui expédie une
décision sur l'**application mécanique** a elle-même été arrêtée par un **contrôle
mécanique** — pas par quelqu'un qui se souvenait. C'est la norme que tout le cours défend,
appliquée à la paperasse du cours lui-même : *le garde-fou que tu exécutes bat la
discipline que tu comptes avoir.*

## Les principes

```text
Une gate qui refuse devrait enregistrer ce qu'elle a refusé.
```

1. **Le fail-open sur la connaissance est le défaut le plus dur à voir**, parce que le
   système a l'air de fonctionner — le garde-fou s'est bel et bien déclenché. Cherche-le
   partout où du code jette une entrée qu'il a décidé de ne pas croire.
2. **Une machine à états, ce sont ses fonctions de transition.** Les états sont décoratifs ;
   si tu ne peux pas nommer une fonction déterministe qui déplace le jeton, tu n'as pas pris
   la décision.
3. **Dérive plutôt que stocker, quand la chose stockée est une fonction de ce que tu gardes
   déjà.** Ça supprime une classe de bug au lieu d'ajouter un contrôle pour elle — et le
   chemin de lecture reste sans écriture.
4. **Surveille la porte dérobée de l'autorité** : une décision tranchée à une gate et
   rouverte par un coefficient. Les gates sont relues ; les flottants dans les formules, non.
5. **Une assertion a besoin de ses deux bras.** « X est absent » est satisfait par un dépôt
   où X n'a jamais été construit. Associe-le à « Y est présent ».
6. **Cite des noms, pas des numéros de ligne.** Une référence qui pourrit à la prochaine
   insertion est de la dérive avec deux-points.

## Checklist de fin

- [ ] Je sais définir le *fail-open sur la connaissance* et dire pourquoi des tests verts ne l'ont pas attrapé.
- [ ] J'ai reproduit le refus silencieux à `v0.22.5` et lu les deux lignes de log.
- [ ] J'ai rempli moi-même le tableau de la machine à états avant de lire la réponse.
- [ ] J'ai appliqué l'instrument aux six décisions de l'ADR 0005 et vérifié chacune.
- [ ] Je sais expliquer pourquoi dériver dissout « qui déplace le jeton ? » au lieu d'y répondre.
- [ ] Je sais énoncer le lien avec le lazy decay de la couche 17 en une phrase.
- [ ] Je sais expliquer la porte dérobée de l'autorité à quelqu'un qui trouve évident de baisser la confiance.
- [ ] J'ai fabriqué la dérive qu'un drapeau stocké autorise, et vu que rien ne la détecte.
- [ ] J'ai cassé l'assertion d'absence seule et compris pourquoi il lui faut une sœur.
- [ ] Je sais nommer au moins deux limites de cette conception sans les relire.
- [ ] Je suis revenu sur `main` et j'ai supprimé ma sonde.

---

## 🎓 À propos de cette leçon

C'est la première leçon du cours sur un défaut qui **n'a jamais produit de symptôme** —
pas de plantage, pas de test rouge, pas de plainte, pendant quatre couches. Les autres
post-mortems t'apprennent à remonter depuis les dégâts ; celle-ci t'apprend que certains
défauts doivent être trouvés par lecture, et te donne la technique de lecture précise qui
les trouve : *liste ce que le système sait à cet instant, puis liste ce qui survit.*

C'est aussi la réponse à la [leçon 24](../24-the-adr-that-didnt-bind/). Cette leçon se
terminait sur une règle — *un système ne peut étiqueter honnêtement la sortie d'un modèle
qu'en termes de ce qu'il contrôle.* Celle-ci montre la même règle appliquée **avant** que
le code existe, à une conception qui était tentante, bien habillée, et qui aurait échoué au
test. Rien ici n'a demandé d'être malin. Il a fallu exécuter l'instrument que la leçon
précédente t'avait mis dans les mains, sur ta propre décision, tant qu'il était encore bon
marché de la changer.

Retour à l'[index du cours](../README.fr.md).
