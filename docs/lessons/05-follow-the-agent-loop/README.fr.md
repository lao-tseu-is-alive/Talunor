# Leçon 05 — Suivre la boucle de l'agent

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 2 · ~60 min

## Pourquoi cette leçon existe

Tout le reste — mémoire, LLM, outils, sécurité — existe pour servir **un tour de la
boucle de l'agent**. Si tu comprends un seul tour, tout le projet se met en place.
L'astuce est de le lire *avant* qu'il ne devienne complexe. Donc cette leçon se cale
sur **`v0.4.0`**, la première version avec un agent, où la boucle est à son plus
simple : rappeler → construire un prompt → interroger le modèle → mémoriser. Ensuite tu
la regarderas grandir.

## Objectifs pédagogiques

À la fin tu sais :
- tracer un tour de l'entrée utilisateur jusqu'à une réponse stockée, sans lire ligne
  par ligne ;
- nommer deux invariants que la boucle protège et dire pourquoi ils comptent ;
- expliquer comment la boucle a plus tard gagné une phase *outils* (la boucle ReAct) —
  en diffant deux tags toi-même.

## Prérequis

- Leçons 00 et 01 faites.
- Une première heure de [A Tour of Go](https://go.dev/tour) si les channels/goroutines
  sont nouveaux (cette leçon en utilise, en douceur).

## Checkout de la boucle simple

```bash
git checkout v0.4.0     # detached HEAD — lecture seule (voir Leçon 00)
```

> **Fichiers à ce tag** (pas de `docs/atlas.md` ici encore — voici le layout à
> `v0.4.0`) :
>
> ```text
> cmd/talunor/main.go        l'agent interactif, câblé (nouveau à ce tag)
> internal/agent/agent.go    la boucle cognitive  ← cette leçon
> internal/llm/              l'interface Provider + l'adaptateur de streaming (Leçon 04)
> internal/memory/           le store SQLite + les embeddings (Leçons 02–03)
> internal/render/           affiche la réponse streamée dans le terminal
> ```

Lis ce seul fichier :

```text
internal/agent/agent.go     # Turn (le point d'entrée) et learnWhileStreaming
```

## La forme d'un tour (à v0.4.0)

Voici tout `Turn`, et il tient sur un écran :

```go
func (a *Agent) Turn(ctx context.Context, input string) (<-chan llm.Chunk, error) {
    // Recall against the input *before* storing it, so the current message is
    // not retrieved as its own top match.
    hits, err := a.store.Recall(ctx, input, a.cfg.RecallK, a.cfg.RecallMaxDistance)
    if err != nil {
        return nil, err
    }

    // Reason: build the prompt from prior context, then start streaming.
    msgs := a.buildMessages(hits, input)

    // Store the user turn now (it happened regardless of how the reply goes).
    a.short.Add(llm.RoleUser, input)
    if _, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleUser, input); err != nil {
        return nil, err
    }

    stream, err := a.provider.Chat(ctx, msgs, a.cfg.Options)
    if err != nil {
        return nil, err
    }

    // Tee the stream to the caller while accumulating the answer; store it on
    // clean completion.
    out := make(chan llm.Chunk)
    go a.learnWhileStreaming(ctx, stream, out)
    return out, nil
}
```

Sous forme de schéma :

```text
input
  │
  ▼
Recall      a.store.Recall(...)         → souvenirs passés pertinents (par le sens)
  │
  ▼
Build       a.buildMessages(hits, input)→ [prompt système, souvenirs, tours récents, input]
  │
  ▼
Store user  a.store.Remember(...user...)  (le message utilisateur a bien eu lieu)
  │
  ▼
Reason      a.provider.Chat(msgs, opts) → un *stream* live de chunks de réponse
  │
  ▼
Learn       learnWhileStreaming(...)    → transmet les chunks ; à la fin propre,
                                          stocke la réponse de l'assistant
```

`buildMessages` assemble le prompt dans un ordre fixe — prompt système, un bloc de
souvenirs rappelés, les tours récents court terme, puis le nouvel input — et
`learnWhileStreaming` fait la partie astucieuse : il **te transmet chaque chunk**
(pour que tu voies la réponse apparaître en direct) tout en accumulant discrètement le
texte complet, et ne le sauvegarde **que si le stream se termine proprement**.

## Exploration guidée

Trouve chacun de ces points dans le code et sois capable de pointer la ligne :

1. **Le rappel a lieu *avant* que le message utilisateur soit stocké.** Pourquoi ?
   (Indice : lis le commentaire. Que se passerait-il si tu stockais d'abord, puis
   cherchais ?)
2. **Une réponse échouée ou à moitié terminée n'est jamais stockée.** Où
   `learnWhileStreaming` décide-t-il ça ? Pourquoi une réponse partielle est-elle pire
   que pas de réponse ?
3. **Les souvenirs rappelés sont injectés comme message système.** Trouve-le dans
   `buildMessages`. (Garde ça en tête — voir *Comment ça a grandi* ci-dessous.)

## Comment ça a grandi (la récompense)

La boucle que tu viens de lire est le *squelette*. Sur les tags suivants elle a pris du
muscle. Vois-le de tes propres yeux — c'est une commande `git` sûre et hors-ligne :

```bash
git diff v0.4.0 v0.7.0 -- internal/agent/agent.go
```

À **`v0.7.0`**, `learnWhileStreaming` est remplacé par **`runLoop`** — la **boucle
ReAct** : le modèle peut maintenant demander un *outil*, l'agent l'exécute, réinjecte
le résultat comme observation, et rappelle le modèle, jusqu'à ce qu'il réponde (borné
par un cap `MaxToolIters` pour qu'il ne boucle pas à l'infini). La réflexion (distiller
des faits durables) est arrivée à `v0.6.0`.

Deux détails de ce code initial ont été **durcis bien plus tard** — un joli rappel
qu'un dépôt pédagogique montre ses cicatrices honnêtement :

- Le `_, _ = a.store.Remember(...)` best-effort pour le tour assistant (l'erreur est
  avalée) est toujours là aujourd'hui — la Leçon 08 étudie exactement ça.
- Injecter les souvenirs comme message *système* est devenu un problème de
  **prompt-injection** dès que les souvenirs peuvent contenir du texte rappelé
  arbitraire ; ç'a été clôturé et étiqueté comme donnée non fiable en **`v0.10.1`**.
  Compare :
  ```bash
  git diff v0.4.0 v0.10.1 -- internal/agent/agent.go | grep -A4 recalled_memories
  ```

Reviens au code le plus récent une fois fini :

```bash
git switch main
```

## Erreurs fréquentes

- **Essayer de lire l'`agent.go` *actuel* en premier.** Sur `main` il porte aussi la
  boucle d'outils, la porte d'approbation et le traçage debug — pars de `v0.4.0`, puis
  diffe.
- **Lire ligne par ligne.** Vise la *forme* : recall → build → reason → learn. Les
  détails sont commentés là où ça compte.

## Checklist de complétion

- [ ] Je peux tracer un tour : input → recall → build → reason → learn.
- [ ] J'ai trouvé où le rappel a lieu *avant* le stockage, et je sais pourquoi.
- [ ] J'ai trouvé où une réponse partielle/échouée n'est *pas* stockée.
- [ ] J'ai lancé `git diff v0.4.0 v0.7.0 -- internal/agent/agent.go` et je peux dire, en
      une phrase, ce qu'a ajouté `runLoop`.
- [ ] Je suis revenu à `main`.

**Suivant :** [Leçon 06 — Construire ton premier outil](../06-build-your-first-tool/)
(ta première contribution 🛠️ sur `main`).
