# Leçon 11 — Quand la mémoire oublie en silence : provenance des embeddings & observabilité

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** (lecture du code de `v0.11.0`, avec des exécutions 🛠️
sur `main`) · Niveau 3 (avancé) · ~75 min

## Pourquoi cette leçon existe

Un jour, Talunor *a oublié qui était son utilisateur*. Le souvenir était pourtant
toujours dans la base — visible avec `/list` — mais quand l'utilisateur a dit « tu
sais qui je suis », l'agent est resté sans réponse. Rien n'avait planté. Aucune
erreur consignée. Le rappel a simplement renvoyé les mauvais voisins.

Cette leçon retrace ce bug réel, car sa cause est un piège dans lequel tout système
de récupération/embeddings peut tomber, et sa correction enseigne deux idées
durables : **stocker la provenance de tes vecteurs**, et **rendre visible
l'invisible**. C'est la leçon qui a transformé une enquête de plusieurs heures en un
simple coup d'œil d'une commande.

## Objectifs pédagogiques

À la fin tu sais :
- expliquer pourquoi un embedding n'est comparable qu'avec des vecteurs du **même**
  modèle, et ce qui casse silencieusement quand le modèle change ;
- décrire comment une **empreinte par vecteur canari** détecte ce changement au
  démarrage ;
- utiliser `talunor --reembed` pour réaligner une base qui a dérivé ;
- activer la trace `/debug` et lire un classement de rappel pour diagnostiquer
  « pourquoi il ne s'en est pas souvenu ? ».

## Prérequis

- Leçon 02 (mémoire persistante) et **Leçon 03 (embeddings & distance cosinus)** —
  cette leçon s'appuie directement dessus.
- La Leçon 08 (observabilité) aide pour la seconde moitié.

## Partie 1 — le bug, tel qu'il s'est produit

Imagine la base après quelques sessions : un `turn` d'il y a des semaines, *« hy my
name is Carlos and i like to develop in Go and Typescript »*, y est stocké bien au
chaud. Plus tard, l'utilisateur demande, vaguement, *« hello you know who i am »*.
Le rappel s'exécute, et le souvenir Carlos n'est **pas** dans les résultats.
Pourquoi ?

Le rappel fonctionne en encodant la requête et en cherchant les vecteurs stockés
les plus proches par distance cosinus (Leçon 03). Il n'y a donc que deux pièces
mobiles : le vecteur de la requête, et les vecteurs stockés. La requête est encodée
à l'instant. Le souvenir Carlos a été encodé **il y a des semaines**. Si le modèle
qui a produit ces deux vecteurs n'est pas le *même*, leur distance n'a aucun sens —
et c'est exactement ce qui s'était passé. Le fichier du modèle d'embeddings (jadis
téléchargé depuis une URL *mutable* — voir le verrou de somme de contrôle ajouté en
`v0.9.1`) avait été remplacé par une autre version. Même dimension, même table,
**espace vectoriel différent**.

Tu peux sentir le problème avec un seul nombre. Encode la *même* phrase deux fois
avec le modèle actuel et les deux vecteurs sont identiques octet pour octet
(l'encodage est déterministe — distance cosinus `0.000000`). Mais l'*ancien* vecteur
*stocké* de cette même phrase se trouvait à une distance cosinus d'**~0,17** d'un
vecteur fraîchement calculé. Pas zéro. Les vecteurs avaient dérivé vers des espaces
différents, et le KNN les a silencieusement mal classés.

> **L'idée centrale.** Un embedding est une coordonnée dans un espace que *seul ce
> modèle* définit. Des vecteurs de deux versions de modèle partagent une forme mais
> pas un sens. Les comparer produit des distances d'apparence plausible mais tout
> simplement fausses — le pire genre de bug, parce que rien ne lève d'erreur.

## Partie 2 — le garde-fou (à lire à `v0.11.0`)

La correction enregistre une empreinte de la pile d'embeddings *dans la base* et la
vérifie à chaque ouverture du store. Lis le code tel qu'il est arrivé :

```bash
git checkout v0.11.0        # detached HEAD — lecture seule (voir Leçon 00)
```

Ouvre le nouveau fichier :

```text
internal/memory/provenance.go
```

Parcours-le et repère ces éléments :

- **Une table annexe `meta`** (`metaSchemaSQL`) — un petit magasin clé/valeur à côté
  de `memories`. Elle contient trois choses : le nom de fichier du modèle, la
  dimension des embeddings, et — la plus importante — un **vecteur canari**.
- **Le canari** (`embedCanaryText`) — une phrase fixe qui est encodée et dont le
  vecteur est stocké. À l'`Open` suivant, le store réencode cette même phrase et
  compare le nouveau vecteur au vecteur sauvegardé. Tout changement de la pile
  d'embeddings — le fichier du modèle, sa config, voire la version de l'extension —
  déplace le canari, si bien que la comparaison les attrape *tous*, pas seulement un
  fichier renommé. (C'est pourquoi un canari vaut mieux qu'un hash du fichier
  modèle : il empreinte le *comportement*, pas les octets.)
- **`ProvenanceStatus`** — les trois issues :
  - `ProvenanceOK` — store neuf, ou le canari correspond → le rappel est fiable.
  - `ProvenanceStale` — le canari ne correspond plus → le modèle a changé ; les
    anciens vecteurs sont dans un espace mort.
  - `ProvenanceUnknown` — le store a des souvenirs mais *aucun* canari enregistré,
    c.-à-d. qu'il précède cette fonctionnalité et ne peut pas être vérifié.
- **`initProvenance`** — appelée à la fin de `bootstrap` (voir `store.go`). Elle
  compare (ou, sur un store neuf, estampille) l'empreinte et fixe le statut.
- **`ReEmbed`** — la migration : elle recalcule chaque vecteur stocké avec le modèle
  actuel et réestampille l'empreinte. Note la forme de la boucle — elle lit **toutes**
  les lignes dans un slice et ferme le curseur *avant* d'encoder. Ce n'est pas un
  choix de style : le store fixe le pool à une seule connexion (état du modèle par
  connexion, voir le piège de la Leçon 02), si bien qu'un curseur `rows` encore
  ouvert bloquerait la requête `Embed`. Une requête imbriquée sur un pool à une
  connexion est un blocage auto-infligé.

Jette aussi un œil au `cosineDistanceBlob` en Go pur — il décode deux BLOBs FLOAT32
et renvoie `1 − produit scalaire` (les vecteurs sont normalisés, donc le produit
scalaire *est* la similarité cosinus). C'est la même distance qu'a expliquée la
Leçon 03, écrite à la main.

Quand tu as fini de lire, reviens :

```bash
git switch main
```

### Expérience — voir le garde-fou à l'œuvre

Le test de provenance déroule tout le cycle dérive→réencodage de façon déterministe
(pas besoin de changer de modèle) :

```bash
go test ./internal/memory/ -run TestProvenanceStaleThenReEmbed -v
```

Lis ce test à côté de `provenance.go` : il stocke des faits, corrompt le canari pour
simuler un changement de modèle, rouvre (→ `ProvenanceStale`), lance `ReEmbed`, et
rouvre encore (→ `ProvenanceOK`). C'est le bug et sa correction, en vingt lignes.

Et sur une base neuve, tu peux voir le statut sain directement :

```bash
make doctor
# • embedding model: all-MiniLM-L6-v2.f16.gguf (dim 384), provenance: ok
```

## Partie 3 — la correction en pratique : `--reembed`

Quand le garde-fou se déclenche sur une vraie base, l'application n'échoue pas —
elle **avertit** au démarrage et pointe vers le correctif. Lancer la migration
réaligne chaque ancien vecteur dans l'espace du modèle actuel :

```bash
go run ./cmd/talunor --reembed
# re-embedding all memories with all-MiniLM-L6-v2.f16.gguf (dim 384)…
#   10/10
# ✓ re-embedded 10 memories (provenance: unknown … → ok)
```

Après ça, le rappel des anciens souvenirs remarche, parce qu'ils vivent désormais
dans le même espace que les requêtes. `/mem` indiquera `provenance: ok`.

## Partie 4 — rendre visible l'invisible : `/debug`

Le bug a été difficile à trouver pour une raison : **on ne pouvait pas voir le
classement de rappel**. L'agent le traçait depuis `v0.9.1`, mais seulement vers un
fichier journal (`TALUNOR_DEBUG`). `v0.11.0` ajoute un interrupteur interactif.
Lis-le :

```text
internal/agent/debug.go     # le bascule /debug et le formatage de la trace
```

L'astuce est volontairement modeste : elle n'invente **pas** un nouveau sous-système
de rendu. Les notes de debug empruntent le canal `Reasoning` *existant* de
`llm.Chunk` — le même canal que l'activité des outils utilise déjà — que les deux
interfaces affichent en atténué. Une seule bascule t'offre donc une visibilité
inline sans aucun changement de rendu. (Le texte de la réponse est accumulé
uniquement depuis `Content`, donc ces notes ne polluent jamais la réponse stockée ni
l'entrée de la réflexion.)

### Expérience — lire un classement de rappel en direct

Nécessite Ollama. Lance le REPL et active le debug :

```bash
go run ./cmd/talunor --plain
```

```text
you> /debug on
you> what is my name and what do i like?
```

Tu verras, en atténué, exactement ce qui a façonné la réponse :

```text
· recall: q="what is my name and what do i like?" k=8 max≤0.75 → 3 hit(s)
·     #13 d=0.5154 turn "write me a hello [my name here]…"
·     #1  d=0.6324 turn "hy my name is Carlos and i like to develop in Go…"
· reflect: extracted 0, stored 0, skipped 0
```

Cette vue — la requête, le budget (`k`, le seuil de distance) et chaque résultat
avec sa distance et son type — c'est tout le diagnostic. Un souvenir situé *juste*
au-dessus de la ligne `max≤0.75` est présent mais exclu ; un souvenir totalement
absent signifie que l'embedding ne l'a pas classé. Si `/debug` avait existé à
l'époque, le bug Carlos aurait été un coup d'œil de dix secondes au lieu d'une
après-midi.

## Les principes

```text
Un embedding sans l'identité de son modèle est un nombre sans unité.
```

1. **Stocke la provenance avec tes données.** Les vecteurs ne sont comparables qu'au
   sein de l'espace d'un seul modèle ; enregistre quel modèle les a produits et
   vérifie-le, sinon la dérive corrompra le rappel en silence.
2. **Verrouille les artefacts téléchargés par empreinte dès le premier jour.** La
   cause racine était un modèle tiré d'une URL mutable avant l'existence des sommes
   de contrôle. Une somme de contrôle protège *vers l'avant* ; elle ne peut pas
   corriger des vecteurs déjà écrits par l'ancien fichier.
3. **La meilleure observabilité est celle déjà câblée.** Les événements existaient ;
   les rendre *visibles* inline était une bascule, pas un sous-système.
4. **Non bloquant ≠ invisible** (Leçon 08, encore) : l'application continue quand la
   provenance est en défaut, mais elle le dit, fort, une fois.

## Checklist de complétion

- [ ] Je peux expliquer pourquoi comparer des embeddings de deux modèles différents
      est faux même si les distances semblent normales.
- [ ] J'ai lu `provenance.go` à `v0.11.0` et je peux dire ce que détecte le vecteur
      canari.
- [ ] J'ai lancé `TestProvenanceStaleThenReEmbed` et suivi le cycle
      dérive→réencodage.
- [ ] Je peux dire pourquoi `ReEmbed` lit toutes les lignes avant d'encoder (une
      seule connexion).
- [ ] J'ai activé `/debug` et lu un classement de rappel avec les distances.
- [ ] Je peux énoncer le principe : un embedding n'a pas de sens sans l'identité de
      son modèle.
- [ ] Je suis revenu à `main`.

---

## 🎓 À propos de cette leçon

C'est la leçon la plus récente, et la première tirée d'un **vrai bug corrigé dans
l'histoire même du projet** plutôt que d'une couche planifiée — la preuve que le
cours grandit avec Talunor. Si tu as fait 00–10, tu comprends maintenant aussi
comment un système de récupération en production échoue *en silence*, et comment
l'attraper. Cet instinct — *« rien n'a levé d'erreur, donc méfie-toi de tout »* —
vaut plus que n'importe quelle fonctionnalité isolée.

Retour à l'[index du cours](../).
