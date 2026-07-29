# Leçon 23 — Deux façons de retrouver une mémoire : quand le sens est le mauvais index

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** (Layer 22, à `v0.21.0`) · Niveau 3 (avancé) · ~75 min

## Pourquoi cette leçon existe

La leçon 03 t'a appris l'astuce qui rend la mémoire de Talunor intelligente : stocker une
phrase comme un *vecteur*, et pouvoir la retrouver par le **sens** — « Quelle technologie
garde une base de données entière dans un seul fichier ? » retrouve « SQLite stores an
entire relational database in a single file » sans partager un seul mot.

Maintenant, dis à l'agent que ta référence de contrat est `AFF-2024-113`, puis redemande-la.

Échec. Non pas parce que la mémoire manque, mais parce qu'**un identifiant n'a pas de
sens**. `AFF-2024-113` et `AFF-2024-114` atterrissent presque exactement au même endroit
de l'espace vectoriel — le modèle n'a aucune idée que l'un des deux est le tien — et
aucun ne se trouve près de la phrase que tu as tapée pour le chercher. La propriété même
qui rendait le rappel malin est celle qui perd la valeur que tu avais le plus besoin de
stocker à la lettre.

La réponse du Layer 22 n'est pas un meilleur modèle d'embedding. C'est un second index au
biais opposé, et une manière honnête de les combiner.

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer ce qu'un embedding ne peut pas représenter, et pourquoi « prends un plus gros
  modèle » ne corrige pas cela ;
- décrire BM25 en une phrase, et dire pourquoi un index inversé a exactement les forces
  qui manquent à un index vectoriel ;
- expliquer pourquoi deux listes classées aux **scores incomparables** doivent être
  fusionnées par *rang* et non par score — et nommer le piège que cela crée quand il n'y
  a qu'une seule liste ;
- tracer la frontière entre *récupération* (« qu'est-ce qui pourrait m'aider à répondre ? »)
  et *identité* (« est-ce le même fait ? »), et dire laquelle des deux a le droit
  d'utiliser la correspondance de mots ;
- reconnaître une capacité qui dépend de la façon dont un binaire a été **compilé**, et
  traiter les balises de build comme faisant partie du contrat d'une fonctionnalité.

## Prérequis

- **Leçon 03 (rappel sémantique)** — embeddings, distance cosinus, le seuil KNN.
- **Leçon 18 (saillance)** — le score `similarité × confiance × saillance` que cette
  couche doit préserver.
- Utile : **leçon 22**, dont cette couche est le premier vrai usager du contrat de capacité.

## Partie 1 — ressens le manque d'abord

```bash
git checkout v0.21.0        # HEAD détachée — lecture seule (voir leçon 00)
make deps                   # si ce n'est pas déjà fait
```

Avant de lire du code, observe l'échec qui motive la couche. Coupe le bras lexical, pour
que le store se comporte exactement comme au Layer 17 :

```bash
TALUNOR_RECALL=vector make run
```

```
you> My contract reference for the school renovation is AFF-2024-113.
you> /quit
```

Relance, et demande :

```bash
TALUNOR_RECALL=vector make run
```

```
you> What is AFF-2024-113?
```

Refais ensuite les deux étapes **sans** `TALUNOR_RECALL=vector` et compare. La différence
n'est pas subtile, et `/debug` te montre exactement pourquoi :

```
recall: q="What is AFF-2024-113?" k=4 max≤0.75 → 1 hit(s)
    #7 v#1 d=0.5340 l#1 score=0.030 fact "The contract reference … is AFF-2024-113."
```

`v#1 … l#1` signifie que les deux bras l'ont trouvé. Essaie une requête où un seul y
arrive, et la notation prend tout son intérêt.

> **L'idée centrale.** Un embedding est un *résumé du sens, avec perte*. C'est une qualité
> quand tu cherches par idée et un défaut quand la chose stockée ne contient aucune idée —
> un identifiant, un numéro de série, une version, un code d'erreur, un nom propre rare.
> Ce sont précisément les faits qu'un assistant personnel doit rendre **au mot près, ou pas
> du tout**.

## Partie 2 — le biais opposé

Lis `internal/memory/lexical.go`. Le bras lexical est un index **FTS5** de SQLite :

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    content='memories',        -- contenu externe : l'index seul, pas de copie du texte
    content_rowid='id',
    tokenize="unicode61 remove_diacritics 2"
);
```

Trois décisions en quatre lignes :

- **`content='memories'`** — un index à *contenu externe* ne stocke que l'index inversé et
  lit le texte depuis `memories`. Pas de contenu dupliqué, donc aucune possibilité que la
  copie dérive de l'original.
- **`remove_diacritics 2`** confond `Genève` et `Geneve`.
- **Pas de stemmer.** Le tokenizer `porter`, évident, améliorerait le rappel anglais et
  massacrerait le français — or cette mémoire contient les deux. Un tokenizer est une
  décision de langue ; le rendre silencieusement anglophone serait un bug qu'on ne
  remarque que dans l'autre langue.

Le classement est **BM25**, en une phrase : *un document marque d'autant plus de points
qu'il contient de termes de ta requête, et beaucoup plus quand ces termes sont rares dans
le corpus.* Cette seconde moitié — la fréquence documentaire inverse — est toute la raison
d'être de ce bras. `AFF-2024-113` n'apparaît que dans une mémoire : il domine donc tout
score qu'il touche.

### La requête n'est pas une chaîne

Tu ne peux pas donner du texte utilisateur à `MATCH`. FTS5 a son propre langage de requête
— `AND`, `OR`, `NOT`, `NEAR`, `*`, `^`, `"…"` — donc `what's my name?` est une **erreur de
syntaxe**, et `cat AND dog` veut silencieusement dire ce que l'utilisateur n'a jamais
demandé. `matchExpression` tokenise, met chaque terme entre guillemets (ce qui neutralise
tout opérateur) et joint par `OR` :

```go
matchExpression("cat AND dog NOT bird")   // → `"cat" OR "dog" OR "bird"`
```

`OR` plutôt que `AND` est délibéré : avec `AND`, un mot absent rejette le document ; avec
`OR`, l'IDF de BM25 fait la discrimination tout seul.

## Partie 3 — le bug qui prouve que les mots vides sont une correction de justesse

C'est la partie que les tests ne pouvaient pas m'apprendre. Le premier essai de bout en
bout du rappel hybride, sur cinq mémoires ordinaires, a produit ceci :

```
q="what language does he like?" → 1 hit
   [l#1 score=0.0148] The user's name is Cedric and he works in Lausanne.
```

Mauvaise réponse, classée avec assurance, venue du seul bras lexical. Il avait accroché
sur le pronom **`he`**.

Prends le temps de voir pourquoi c'est pire qu'un échec :

1. BM25 *dévalue* les termes courants ; il ne les **refuse** pas. Avec `OR`, un seul terme
   parasite suffit à admettre un document.
2. Le bras vectoriel a une barrière de pertinence — `maxDistance` — donc une mémoire hors
   sujet est écartée. Le bras lexical n'a **aucun équivalent** : la correspondance est
   binaire, et le classement ne fait qu'ordonner ce qui a correspondu.
3. Résultat : le bras sans barrière a produit un premier résultat assuré pour une question
   à laquelle le bras avec barrière avait honnêtement répondu *rien*.

Le correctif est une liste de mots vides (anglais **et** français), plus une règle qui
l'empêche de dévorer les jetons mêmes qui justifient cette couche :

```go
func keepTerm(t string) bool {
	if hasDigit(t) {          // le "16" de "PostgreSQL 16.2" est la partie discriminante
		return true
	}
	return len([]rune(t)) >= minTermLen && !stopwords[t]
}
```

Lis `TestMatchExpressionDropsFunctionWords` — c'est cet échec réel, figé. Et note le détail
à voler dans la liste : elle contient **à la fois** `etait` et `était`, parce que le
tokenizer de FTS5 supprime les accents alors que ce filtre Go voit la requête brute. Deux
composants, deux vues de la même chaîne.

> Un bras lexical qui se déclenche sur les mots outils n'ajoute pas du rappel. Il ajoute
> des mensonges.

## Partie 4 — fusionner deux listes sans échelle commune

Lis maintenant `internal/memory/hybrid.go`. Le bras vectoriel classe par distance cosinus
(bornée, `0` = identique) ; le bras lexical par BM25 (négatif, non borné, relatif au
corpus). Demande-toi ce que valent l'un par rapport à l'autre `0.7 × (1 - 0.53)` et
`-1.06e-06`.

Il n'y a pas de réponse. Tout mélange pondéré est une constante que quelqu'un règle à
l'infini, et qui se dérègle dès que le corpus grossit. La **fusion par rang réciproque**
(RRF) esquive la question en jetant les scores pour ne garder que l'*ordre* de chaque bras :

```go
rrf(mémoire) = Σ sur les bras  1 / (rrfK + rang_dans_ce_bras)      // rrfK = 60
```

Deux propriétés en découlent gratuitement :

- **La corroboration gagne.** Une mémoire classée par les deux bras cumule les deux, sans
  que personne ne choisisse un poids.
- **La tête est aplatie.** `rrfK = 60` fait que le rang 1 vaut à peine plus que le rang 2 —
  ce qu'on veut quand un bras se trompe parfois avec assurance (la partie 3 montre qu'il le
  peut).

Confiance et saillance ne bougent pas : `score = rrf × confiance × saillance-effective`.
La pertinence a changé de forme ; la *confiance* et l'*importance* gardent le sens que les
leçons 17 et 18 leur ont donné.

### Le piège : RRF n'est pas la fonction identité

Voici la subtilité par laquelle il est facile de livrer un bug. Sur un build sans FTS5 il
n'y a qu'une liste — donc la fusion devrait être neutre, non ?

Non. Classer par `1/(60+rang) × conf × sal` n'est **pas** le même ordre que
`(1-distance) × conf × sal`, parce que les deux termes de pertinence décroissent
différemment. Fais le calcul toi-même :

| hit | distance | confiance | `(1-d)·conf` | rang | `1/(60+rang)·conf` |
|-----|----------|-----------|--------------|------|--------------------|
| A   | 0.10     | 0.5       | **0.45**     | 1    | 0.0082             |
| B   | 0.70     | 1.0       | 0.30         | 2    | **0.0161**         |

Le score classique met **A** en tête ; RRF met **B**. Mêmes données, même chemin de code,
réponse différente — et tous les utilisateurs qui n'ont jamais demandé le rappel hybride
auraient silencieusement eu la seconde. `fuse` traite donc le cas à un seul bras à part et
garde la formule du Layer 17 telle quelle, ce que fige
`TestFuseWithOneArmKeepsLayer17Ranking`.

**La leçon générale :** quand tu généralises un mécanisme, vérifie que la nouvelle
implémentation se réduit *exactement* à l'ancienne sur les anciennes entrées. « C'est un
cas particulier du nouveau truc » est une affirmation à vérifier, pas à supposer.

## Partie 5 — la récupération est hybride ; l'identité est métrique

La frontière qui a coûté une régression, et l'idée la plus transférable ici.

`Recall` et `RecallForConsolidation` ressemblent à la même fonction avec un drapeau. Elles
posent des questions fondamentalement différentes :

| | question | bon outil |
|---|---|---|
| `Recall` | *qu'est-ce qui pourrait m'aider à répondre ?* | **hybride** — un candidat de plus coûte peu, un identifiant manqué coûte cher |
| `RecallForConsolidation` | *est-ce que je détiens déjà ce fait ?* | **vectoriel seul** — c'est une question de distance |

Brancher le bras lexical sur les deux a cassé la réflexion (la machinerie de la leçon 20).
Deux phrases peuvent partager le jeton rare `NX-9000` et dire des choses entièrement
différentes :

```
"The Lausanne office network switch is model NX-9000."
"The NX-9000 firmware upgrade is scheduled for the winter break."
```

BM25 les classe comme quasi identiques ; la distance cosinus sait que non. Quand le
recouvrement de mots a eu le droit de proposer des candidats de consolidation,
`learnOneFact` s'est mis à fusionner de nouveaux faits sur des faits simplement proches par
les mots — et *à ne plus les stocker du tout*. Le `maxDistance` de l'appelant est un
**rayon cosinus**, et un hit lexical n'y a aucune coordonnée.

Lis `TestConsolidationLookupIgnoresLexicalOverlap`. Puis note comment le bug a été trouvé :
non par un nouveau test, mais par un **test existant du Layer 20** qui n'avait rien à voir
avec cette couche. Les vieux tests gagnent leur place exactement à ce moment-là.

## Partie 6 — une capacité qui vit dans le build

Tout ce qui précède dépend d'un fait invisible dans le source et invisible dans `go.mod` :

```bash
go test ./internal/memory/ -run TestHybridRecall -v      # sans balise
# --- SKIP: fts5 capability unavailable: built without -tags sqlite_fts5
```

`mattn/go-sqlite3` compile SQLite lui-même, et **FTS5 seulement sous `-tags sqlite_fts5`**.
Le build par défaut a FTS3/4, pas de module `fts5`, pas de `bm25()`. C'est donc la première
capacité de Talunor qui dépend de la *façon dont le binaire a été compilé* plutôt que de ce
qui est installé sur la machine — et une capacité silencieuse, puisqu'un build sans balise
tourne parfaitement, en récupérant simplement moins.

Trois conséquences, toutes visibles dans le dépôt :

1. La balise voyage sur **tous** les builds supportés : `GOTAGS` dans le Makefile, le
   Dockerfile, `release.yml`. Oublies-en un et le binaire *livré* perd discrètement la
   fonctionnalité.
2. La dégradation est **signalée, pas silencieuse** : `Store.Lexical()` → `unavailable`,
   affiché par `make doctor` et `/mem`.
3. L'index FTS5 n'est **pas une migration**. Chaque octet est reconstructible depuis
   `memories`, et un build sans balise *ne peut pas le créer du tout* — le mettre dans la
   liste ordonnée des migrations ferait donc promettre à `schema_version` quelque chose que
   la base pourrait être incapable d'honorer. Il est créé de façon idempotente à `Open`,
   comme `vector_init`. **Les migrations sont pour les données sources ; les index dérivés
   se reconstruisent.**

Et c'est là que le contrat de la leçon 22 est payant, une version plus tard :

```bash
TALUNOR_REQUIRE=fts5 go test ./internal/memory/ -run TestHybridRecall
# --- FAIL: TALUNOR_REQUIRE=fts5 declares this host must be able to exercise "fts5",
#     but it cannot: lexical arm is unavailable (built without -tags sqlite_fts5)
```

La CI déclare `TALUNOR_REQUIRE=ext,fts5` : un build qui perdrait la balise fait rougir ces
tests au lieu de les sauter derrière un `ok` vert.

## Pratique — casse chaque moitié et regarde ce qui meurt

```bash
# 1. Stocke une phrase porteuse de sens et un identifiant, puis interroge des deux façons.
make run
#   you> The production database runs PostgreSQL 16.2 on the blue cluster.
#   you> /debug
#   you> PostgreSQL 16.2           → observe les bras dans la trace
#   you> which database do we use? → observe-les à nouveau
#   you> /quit

# 2. Tue le bras lexical ; recommence. La requête identifiant se dégrade, pas la sémantique.
TALUNOR_RECALL=vector make run

# 3. Tue plutôt le bras SÉMANTIQUE — modifie recall() pour sauter vectorCandidates, puis :
go test -tags sqlite_fts5 ./internal/memory/ -run 'TestHybridKeepsSemanticRecall'
#    Il échoue : « Which technology keeps a whole database in one file ? » ne partage aucun
#    mot avec la mémoire SQLite. Chaque bras est porteur pour une question différente.

# 4. Retire un mot vide — supprime "he" de la map — et relance :
go test -tags sqlite_fts5 ./internal/memory/ -run 'TestMatchExpressionDropsFunctionWords'
#    Une entrée de map supprimée, c'est la différence entre « pas de réponse » et une
#    réponse fausse et assurée.

# 5. Remets tout en état :
git checkout internal/memory/
```

## Les principes

```text
Deux index aux biais opposés valent mieux qu'un index avec un meilleur modèle.
```

1. **Sache ce que ton index ne peut pas représenter.** Les embeddings perdent le littéral ;
   ce n'est pas un problème de qualité à régler avec un plus gros modèle, c'est un problème
   de *nature* à régler avec un second index.
2. **Fusionne des rangs, pas des scores,** quand les scores n'ont pas d'échelle commune — et
   vérifie ensuite que la fusion se réduit exactement à l'ancien comportement quand il n'y a
   qu'une liste.
3. **Un matcher sans barrière de pertinence en réclame une, écrite à la main.** BM25 classe
   ce qui a correspondu ; décider de ce qui a le droit de correspondre est ton travail
   (mots vides, règles sur les termes).
4. **Récupération et identité sont deux questions différentes.** Le recouvrement de mots
   peut proposer des candidats pour « qu'est-ce qui pourrait aider ? » ; seule la distance
   peut répondre à « est-ce la même chose ? ».
5. **Les index dérivés se reconstruisent, ils ne se migrent pas.** La version de schéma ne
   doit jamais promettre ce qu'un build donné ne peut pas créer.
6. **Les balises de build font partie du contrat.** Une capacité qui dépend d'options de
   compilation doit être détectée à l'exécution, signalée quand elle manque, et déclarée par
   quiconque prétend la tester.

## Checklist de fin

- [ ] J'ai reproduit l'échec sur l'identifiant avec `TALUNOR_RECALL=vector`, et le succès sans.
- [ ] Je sais expliquer l'IDF de BM25 en une phrase et pourquoi `OR` vaut mieux qu'`AND` ici.
- [ ] Je sais dire pourquoi le pronom « he » a produit un résultat faux et assuré, et ce que
      corrige une liste de mots vides (un problème de *justesse*, pas de performance).
- [ ] J'ai refait le tableau RRF à la main et je peux expliquer pourquoi le cas à un bras est
      traité à part.
- [ ] Je sais énoncer la frontière récupération/identité et quel chemin de rappel est
      vectoriel seul.
- [ ] J'ai lancé la suite sans `-tags sqlite_fts5` et vu les tests sauter, puis avec
      `TALUNOR_REQUIRE=fts5` et vu échouer.
- [ ] J'ai fait au moins les expériences 3 et 4, et je suis revenu sur `main`.

---

## 🎓 À propos de cette leçon

Elle referme l'arc de l'Itération 5 — une mémoire qui apprend de l'action (20), se corrige
(21), et sait désormais *retrouver* ce qu'elle détient (22) — et c'est l'exemple le plus net
du cours d'un thème récurrent : **le correctif honnête d'une limite est presque toujours un
second mécanisme aux modes de défaillance opposés, pas une meilleure version du premier.**
Tu as déjà croisé cette forme en leçon 12 (une policy à côté du jugement du modèle) et en
leçon 16 (un vérificateur déterministe à côté de la réponse d'un LLM). Vectoriel plus
lexical, c'est le même réflexe appliqué à la récupération.

L'autre chose à emporter : deux des trois bugs les plus coûteux de cette couche ont été
trouvés en *l'exécutant*, pas en la testant. Le match sur le pronom et la régression de
consolidation étaient invisibles en relecture et évidents après cinq minutes d'usage réel.

Retour à l'[index du cours](../README.fr.md).
