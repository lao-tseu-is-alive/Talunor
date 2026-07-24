# Leçon 18 — La mémoire du geste : saillance, décroissance & consolidation

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration + pratique** (lire `internal/memory` et `internal/agent` ; le code du
Layer 17 est sorti à `v0.17.0`, docs de référence sur `main`) · Niveau 3 (avancé) · ~75 min

## Pourquoi cette leçon existe

La Leçon 17 a donné à un souvenir une *confiance* — d'où il vient et à quel point y
croire. Mais elle laissait chaque souvenir aussi *vivant* que les autres : un fait
mentionné une fois il y a un an et un fait répété chaque jour étaient côte à côte, et le
rappel traitait le store comme un tas indifférencié. Ce n'est pas ainsi que fonctionne
la mémoire, ni pour les gens ni pour un bon agent.

Voici un parallèle que tu as déjà ressenti. Quand une longue conversation remplit la
fenêtre de contexte, tu lances `/compact` : l'assistant **consolide** de nombreux tours
en un résumé court et **laisse s'estomper le détail trivial**. C'est exactement ce que
ce layer apprend au store *long-terme* de Talunor — mais en continu et graduellement,
pas en une salve unique et avec perte sous la pression. La mémoire de travail (la
fenêtre de contexte, le ring buffer court-terme) est compactée quand elle déborde ; la
mémoire long-terme (le store SQLite) reçoit sa propre **saillance** pour, elle aussi,
garder ce qui compte et laisser le reste s'estomper.

Le nom biologique de la version utile de tout ça est la consolidation : ce qui est
ré-activé est renforcé, ce qui reste inutilisé s'efface. Le Layer 17 donne au store deux
forces — la **décroissance** (s'estomper par négligence) et le **renforcement**
(se renforcer par l'usage et la répétition) — et, surtout, le fait sans trahir la règle
d'honnêteté bâtie en Leçon 17.

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer pourquoi un store de mémoire indifférencié dégrade le rappel, et ce que la
  **saillance** ajoute par-dessus la pertinence et la confiance ;
- décrire la **décroissance paresseuse** et dire pourquoi la calculer au moment de la
  lecture (plutôt qu'un balayage de fond) est le design qui respecte la connexion unique ;
- lire comment le rappel **classe** désormais par `similarité × confiance × saillance-effective`
  et **oublie en douceur** les souvenirs estompés sans les supprimer ;
- expliquer la **consolidation** — pourquoi un fait ré-énoncé renforce une ligne
  existante au lieu d'empiler un doublon — et la **règle d'indépendance** qui la garde honnête ;
- poser les leviers et voir un souvenir s'estomper, être ranimé, et se renforcer.

## Prérequis

- **Leçon 05** (la boucle de l'agent) — où vivent le rappel et la réflexion.
- **Leçon 17** (apprendre avec humilité) — provenance & confiance ; cette leçon bâtit la
  moitié *rétention* par-dessus cette moitié *confiance*, et s'appuie sur la même règle d'honnêteté.
- **Leçon 11** (quand la mémoire oublie) — l'instinct d'observabilité ; ici tu observes
  la saillance et le score dans `/debug` et `/list`.

## Partie 1 — le problème du tas, et une nouvelle colonne

Lis la forme d'un souvenir sur `main` :

```text
internal/memory/memory.go
```

Depuis la Leçon 03, `Recall` renvoyait les *k plus proches* souvenirs par distance
cosinus, plus tard filtrés par un seuil `maxDistance` (la Leçon 17 a ajouté un filtre de
confiance). Pertinence et confiance — mais rien sur **à quel point un souvenir compte
en ce moment**. Un fait rappelé chaque jour et un fait rappelé une fois se classent à
l'identique s'ils sont à la même distance.

Le Layer 17 ajoute cet axe manquant. Lis comment la colonne est arrivée :

```text
internal/memory/migrate.go
```

La **migration 3** ajoute trois colonnes à `memories` : `salience` (défaut `1.0`),
`last_accessed` et `access_count`. C'est encore la règle append-only de la Leçon 17 à
l'œuvre — la migration 3 est *ajoutée*, on n'édite jamais la 1 ni la 2, et les lignes
existantes démarrent pleinement saillantes et non accédées, donc rien de déjà stocké
n'est rétroactivement déclassé. `doctor` affiche maintenant `schema version: 3`.

## Partie 2 — décroissance paresseuse : le design qui respecte la contrainte

Voici maintenant le cœur du layer :

```text
internal/memory/salience.go
```

La saillance d'un souvenir devrait baisser d'autant plus longtemps qu'il reste
intouché. L'implémentation évidente — une tâche de fond qui écrit périodiquement une
saillance plus basse dans chaque ligne — est exactement la mauvaise *ici*, et la raison
est un piège rencontré en Leçon 02 :

> **La contrainte.** Le store épingle `db.SetMaxOpenConns(1)` parce que les extensions
> SQLite gardent le modèle et l'état vectoriel dans un état *par connexion*. Un
> « balayeur » de fond écrivant dans chaque ligne se battrait avec la connexion même que
> les lectures utilisent.

Donc la décroissance est **paresseuse**. On n'écrit jamais rien juste pour faire
s'estomper un souvenir. La `salience` stockée est sa valeur *au moment de*
`last_accessed` ; la saillance **effective** au moment de la lecture en est calculée.
Lis `effectiveSalience` :

```go
// salience × 2^(−age / demi-vie) : après une demi-vie le facteur vaut 0.5, après deux 0.25…
return salience * math.Exp2(-float64(age)/float64(halfLife))
```

Forme en demi-vie car c'est enseignable : après `TALUNOR_SALIENCE_HALFLIFE` (défaut 30
jours) de négligence, un souvenir vaut moitié moins. Le geste élégant est de *ne pas
stocker du tout la valeur décrue* — seulement la saillance-au-dernier-contact — et de la
faire décroître à la sortie. **Recall n'écrit rien**, donc il reste une lecture pure sur
la connexion unique.

Relis maintenant `Recall` dans `memory.go` avec ça en tête. Deux changements :

1. **Classement.** La pertinence reste le *portail* (tours assistant exclus,
   `maxDistance` écarte les non-pertinents), mais dans le voisinage pertinent les
   souvenirs sont désormais ordonnés par un score combiné :

   ```go
   h.Score = (1 - h.Distance) * h.Confidence * eff   // similarité × confiance × combien-ça-compte-maintenant
   ```

   Un souvenir de confiance et renforcé passe devant un souvenir à peine pertinent ou
   estompé depuis longtemps à distance similaire.

2. **Oubli en douceur.** Un souvenir dont la saillance effective est tombée sous
   `ForgetFloor` (défaut `0.05`) est *écarté du rappel* — mais la ligne n'est jamais
   supprimée. Lis le commentaire : elle survit, et un ré-énoncé la ranime. C'est un choix
   délibéré face à la suppression dure : oublier des données personnelles en silence est
   un risque plus grand que garder une ligne pâle qui ne remonte plus.

## Partie 3 — renforcement & consolidation : la mémoire du geste

La décroissance n'est que la moitié de l'histoire ; l'autre moitié est ce qui *renforce*
un souvenir. Lis les deux méthodes de renforcement dans `salience.go` :

- `Reinforce(ids)` — augmente la saillance (plafonnée), incrémente `access_count`, et
  réinitialise l'horloge de décroissance (`last_accessed = now`). Elle touche **la
  saillance seule**.
- `ReinforceFact(id, gain)` — fait tout ce qui précède *et* élève la **confiance** vers
  un plafond sous 1.0, avec des rendements décroissants.

Elles se déclenchent à deux moments bien définis, jamais comme effet de bord de
`Recall` (le rappel est une lecture pure). Trouve-les dans `internal/agent/agent.go` :

- **Au rappel** — `reinforceRecalled` augmente la saillance des souvenirs qui ont façonné
  le prompt d'un tour. Être récupéré et utilisé est un signal qu'un souvenir compte.
- **Au ré-énoncé** — c'est l'évolution de ce qui n'était que de la déduplication. Lis
  `reflect`. Avant, quand l'extracteur produisait un fait déjà dans le store, l'agent le
  *sautait* (`factKnown`). Maintenant il **consolide** : `knownFact` renvoie la ligne
  existante, et `ReinforceFact` la renforce au lieu de stocker un quasi-doublon.

Ce dernier changement est le cœur de la leçon. Un fait énoncé trois fois devient **une
seule ligne de plus en plus digne de confiance et de plus en plus saillante**, pas trois
copies. C'est la *mémoire du geste* : plus un savoir est re-confirmé, plus il compte — de
la même façon qu'un mouvement répété devient une seconde nature.

## Partie 4 — la règle d'indépendance : garder la répétition honnête

Voici le piège, et l'idée la plus importante du layer. « Plus un fait est répété, plus
je lui fais confiance » est juste — **mais seulement si les répétitions sont
indépendantes.** Si le *modèle* ré-énonce sa propre inférence antérieure et que tu
comptes ça comme une confirmation, l'agent bâtit une chambre d'écho auto-renforçante et
se convainc d'une fausse certitude. C'est le piège de sycophancie de la Leçon 17, qui
revient par la porte de côté.

Lis `EvidenceCredibility` dans `salience.go` :

```go
case ProvenanceUserStated, ProvenanceToolObserved:
    return 1.0   // corroboration indépendante et crédible
case ProvenanceModelInferred:
    return 0.0   // le modèle qui s'écoute lui-même — aucun gain de confiance
```

La parade est de **séparer les deux effets**, ce qui est exactement pourquoi la Leçon 17
a gardé saillance et confiance comme axes *distincts* :

- La **saillance** monte à *toute* répétition ou tout rappel. La fréquence signifie
  « ça compte », que ce soit vrai ou non. Aucun risque.
- La **confiance** monte *seulement sur preuve indépendante*. Un utilisateur qui répète
  (ou un outil qui ré-observe) un fait le corrobore ; le modèle qui ré-infère sa propre
  affirmation gagne **zéro** confiance — saillance en hausse, confiance à plat.

Lis où l'agent calcule le gain dans `reflect` :

```go
gain := clamp01(consolidationGainBase * memory.EvidenceCredibility(prov) * a.cfg.ModelConfidence)
```

Remarque que le gain intègre aussi `ModelConfidence` — le même **lien calibration** de
la Leçon 17. Un ré-énoncé venant d'un modèle peu fiable gagne proportionnellement moins.
Et la mise à jour de confiance elle-même (en SQL, reflétée par `reinforcedConfidence`)
ne parcourt jamais qu'une fraction du chemin vers un plafond sous 1.0 : la répétition,
aussi fréquente soit-elle, ne rend jamais une affirmation *certaine*. C'est l'humilité du
Layer 17, préservée jusque dans la rétention.

## Partie 5 — le voir s'estomper, renaître et se renforcer

D'abord les tests de fonctions pures — pas de base, pas d'extensions, donc ils tournent
partout :

```bash
go test ./internal/memory/ -run 'EffectiveSalience|Evidence|Reinforced' -v
```

Lis-les à côté du code : la décroissance divise par deux sur une demi-vie ; la preuve
indépendante compte, le modèle qui s'écoute non ; la confiance monte avec des rendements
décroissants et ne dépasse jamais le plafond.

Puis le comportement adossé au store (nécessite `make deps`) :

```bash
go test ./internal/memory/ -run 'Reinforce|Forget' -v
```

`TestRecallForgetFloorAndRevival` est celui à lire : un fait frais sous le seuil d'oubli
est oublié en douceur (le rappel ne renvoie rien, mais `Count` vaut toujours 1), puis le
renforcer au-delà du seuil le ramène. Oublier sans supprimer, ranimer sans magie.

Maintenant en direct (nécessite Ollama). Rends l'oubli facile à voir en plaçant le seuil
au-dessus de la saillance d'un souvenir frais, et active la trace en session :

```bash
TALUNOR_FORGET_FLOOR=1.4 go run ./cmd/talunor --plain
```

```text
you> /debug on
you> retiens que mon chat s'appelle Ada
you> comment s'appelle mon chat ?
```

Avec le seuil à `1.4` et une saillance fraîche de `1.0`, le fait est stocké mais oublié
en douceur — la trace de rappel de `/debug` le montre disparaître, et l'agent peut ne pas
se souvenir d'Ada. Ré-énonce-le (`mon chat Ada est très vieux maintenant`) et observe la
ligne `reflect: ~fact … reinforced` : la saillance grimpe au-delà du seuil et le souvenir
revient dans le rappel. Lance `/list` et tu verras le fait annoté `(user_stated 90%, sal
1.5×1)` — confiance, saillance et nombre d'accès, tous visibles. C'est l'instinct
d'observabilité de la Leçon 11 appliqué à la rétention : tu peux *voir* pourquoi un
souvenir se classe où il est, ou pourquoi il a disparu.

## Les principes

```text
Garde ce qui sert, laisse s'estomper l'inutilisé — et laisse seule la répétition indépendante bâtir la confiance.
```

1. **La saillance est un troisième axe, aux côtés de la pertinence et de la confiance.**
   Le rappel classe par les trois ; un souvenir qui compte maintenant passe devant un
   souvenir estompé.
2. **Décroître paresseusement.** Calcule-la à la lecture pour que le rappel ne devienne
   jamais une écriture — le design qui respecte la connexion unique. Stocke la
   valeur-au-dernier-contact, pas la valeur décrue.
3. **Oublier en douceur.** Écarte un souvenir estompé du rappel, mais garde la ligne —
   un ré-énoncé la ranime. Ne supprime jamais des données personnelles en silence.
4. **Consolider les ré-énoncés ; la répétition renforce la mémoire.** Mais la saillance
   monte à toute répétition, tandis que la confiance ne monte **que sur preuve
   indépendante** — le garde-fou anti-chambre-d'écho qui garde intacte la règle
   d'honnêteté de la Leçon 17.

## Checklist de complétion

- [ ] Je peux expliquer ce que la saillance ajoute que la pertinence et la confiance n'apportent pas.
- [ ] Je peux dire pourquoi la décroissance est calculée à la lecture, et le lier à `SetMaxOpenConns(1)`.
- [ ] J'ai lu `Recall` et je peux pointer le score `similarité × confiance × saillance`
      et l'écartement par le seuil d'oubli.
- [ ] Je peux expliquer pourquoi un souvenir estompé est oublié en douceur, pas supprimé.
- [ ] J'ai lu `reflect` et je peux décrire comment un ré-énoncé consolide au lieu de dupliquer.
- [ ] Je peux énoncer la règle d'indépendance et pourquoi saillance et confiance restent séparées.
- [ ] J'ai lancé l'agent avec un `TALUNOR_FORGET_FLOOR` élevé, vu un souvenir disparaître, et l'ai ranimé.

---

## 🎓 À propos de cette leçon

Ceci clôt le premier mouvement de l'Itération 4. Suis l'arc : la Leçon 11 a attrapé un
substrat qui se dégradait *en silence* ; la Leçon 16 a *mesuré* la fiabilité d'un modèle ;
la Leçon 17 a *dépensé* cette mesure pour gouverner à quel point l'agent peut croire ; et
cette leçon gouverne quels souvenirs il *garde*. La confiance, puis la rétention — les
deux moitiés d'un apprentissage avec une colonne vertébrale.

Remarque le parallèle avec `/compact` une dernière fois, car il est exact : la compaction
est une consolidation forcée et avec perte de la mémoire *de travail* sous la pression du
contexte ; la saillance est une consolidation continue et gracieuse de la mémoire
*long-terme*. Même instinct — renforcer le ré-activé, estomper le trivial — sur les deux
échelles de temps où vit un agent.

Le layer suivant, la **réflexion asynchrone**, porte sur *quand* l'apprentissage a lieu
plutôt que sur *ce qui* est appris : la réflexion lance aujourd'hui un second appel modèle
sur le chemin critique du tour (tu le ressens comme de la latence). La déplacer vers un
worker de fond — un worker qui devra posséder la connexion unique que tu viens de passer
deux leçons à respecter — c'est le Layer 18.

Retour à l'[index du cours](../).
