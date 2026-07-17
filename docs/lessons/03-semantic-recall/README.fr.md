# Leçon 03 — Rappel sémantique & embeddings

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 2 · **Avancé** · ~60 min

> **Leçon avancée.** Elle touche aux vecteurs et à un peu d'interop C (cgo). Si c'est
> nouveau, survole-la maintenant et reviens plus tard — tu peux tout à fait continuer
> vers la Leçon 04 sans la maîtriser entièrement.

## Pourquoi cette leçon existe

En Leçon 01 tu as vu la magie : demander *« a famous French landmark »* a rappelé le
souvenir *Eiffel Tower*, alors qu'ils ne partagent aucun mot. C'est la **recherche
sémantique** — une correspondance par le *sens*. Cette leçon explique comment ça
marche, sur le même tag que la Leçon 02, **`v0.2.0`**.

## Objectifs pédagogiques

À la fin tu sais :
- expliquer la différence entre recherche par mots-clés et recherche sémantique ;
- décrire ce qu'est un *embedding* et ce que mesure la *distance cosinus* ;
- expliquer le **seuil de distance** et le compromis qu'il contrôle.

## Prérequis

- Leçon 02 faite (même code, autre angle).

## Checkout de la couche mémoire (même tag que la Leçon 02)

```bash
git checkout v0.2.0     # detached HEAD — lecture seule
```

Lis, en te concentrant sur la *recherche* cette fois :

```text
internal/memory/store.go     # Embed et Dim — transformer du texte en vecteur
internal/memory/memory.go    # Recall, et le type Hit (note : Distance float64)
Makefile                     # `deps` télécharge le modèle d'embeddings (le fichier .gguf)
```

## L'idée en trois niveaux

**Intuition.** L'ordinateur transforme chaque bout de texte en une liste de nombres
(un *vecteur*) qui capture son *sens*. Des textes de sens proche obtiennent des
vecteurs *proches les uns des autres* — donc « French landmark » atterrit près de
« Eiffel Tower », loin de « database password ».

**Technique.** Cette liste de nombres est un **embedding** (ici, 384 nombres par
texte, produits localement par le modèle embarqué). La « proximité » se mesure par la
**distance cosinus** : `0` = même direction (même sens), plus grand = plus différent.
`Recall` embed ta requête, la compare à chaque vecteur stocké et renvoie les *k* plus
proches — une recherche des **k plus proches voisins (KNN)**.

**Dans Talunor.** Lis la vraie signature (à `v0.2.0`) :

```go
func (s *Store) Recall(ctx context.Context, query string, k int, maxDistance float64) ([]Hit, error)
// type Hit struct { …; Distance float64 }   // Distance plus petite = plus similaire
```

`Embed(text)` fabrique le vecteur ; `Recall` trouve les `k` plus proches et les renvoie
avec leur `Distance`.

## Le seuil — le bouton qui compte

`Recall` prend un `maxDistance`. Dans le code tu verras à peu près :

```go
if maxDistance > 0 && h.Distance > maxDistance {
    // trop loin — on l'écarte
}
```

Cette seule ligne contrôle toute la qualité du rappel :

| `maxDistance` | Effet |
|---------------|-------|
| trop petit (strict) | rien ne correspond — l'agent « oublie » des choses pertinentes |
| trop grand (permissif) | tout correspond — le bruit inonde le prompt |
| juste bien (~0.75) | seuls les souvenirs vraiment pertinents reviennent |

## Expérience

Lance le smoke test et lis les distances qu'il affiche :

```bash
make doctor
```

```text
• recall: "Which technology keeps a whole database in one file?"  (threshold d≤0.75)
   1. [d=0.2405] SQLite stores an entire relational database in a single file.
• recall: "Tell me about a famous French landmark."  (threshold d≤0.75)
   1. [d=0.6189] The Eiffel Tower was completed in Paris in 1889.
```

Remarque les deux distances : la correspondance SQLite est *très* proche (`0.24`),
celle de la Tour Eiffel plus lâche (`0.62`) mais toujours sous le seuil `0.75` — donc
les deux reviennent, et les souvenirs non liés (qui scoreraient plus haut) non.
**Cet écart, c'est le seuil qui fait son travail.**

Reviens au code le plus récent une fois fini :

```bash
git switch main
```

## Questions auxquelles répondre

- Pourquoi *« French landmark »* rappelle-t-il *« Eiffel Tower »* mais pas *« database
  password »* ? (Réponds en une phrase, en utilisant le mot *sens*.)
- Qu'arriverait-il à la qualité du rappel si tu mettais `maxDistance` à `0.1` ? À
  `2.0` ?
- Pourquoi un simple balayage complet de tous les vecteurs est-il acceptable ici, mais
  un problème si l'agent avait un million de souvenirs ? (C'est une limite réelle et
  documentée — voir le `CHANGELOG.md` sur `main`.)

## Pour aller plus loin

- Le modèle d'embeddings tourne **in-process via une extension C de SQLite** — c'est ce
  dont parlent `cgo_link.go` et cgo. Tu n'as pas besoin de le maîtriser ; sache juste
  *pourquoi* ça existe : aucun service d'embedding externe, tout marche hors-ligne.
- Sur `main`, le même `Recall` a gagné un petit facteur de sur-récupération avant le
  filtrage (`recallCandidateFactor`). Vois si tu peux le trouver — et raisonne sur le
  *pourquoi* : récupérer quelques candidats en plus avant d'appliquer le filtre de rôle
  est plus sûr.

## Erreurs fréquentes

- **Croire que les embeddings « comprennent » le texte.** Ils ne raisonnent pas — ils
  placent le texte dans un espace où *distance ≈ dissimilarité de sens*. C'est
  suffisant, et c'est tout ce que c'est.
- **Traiter le seuil comme universel.** C'est un bouton de réglage ; la bonne valeur
  dépend du modèle et des données.

## Checklist de complétion

- [ ] Je peux expliquer recherche sémantique vs recherche par mots-clés en une phrase.
- [ ] Je peux dire ce qu'est un embedding et ce que mesure la distance cosinus.
- [ ] J'ai trouvé la vérification `maxDistance` dans `Recall` et je peux expliquer les
      deux extrêmes.
- [ ] J'ai lu les deux distances de la sortie `make doctor` et compris pourquoi les
      deux correspondaient.
- [ ] Je suis revenu à `main`.

**Suivant :** [Leçon 04 — Provider LLM & streaming](../04-llm-provider-and-streaming/).
