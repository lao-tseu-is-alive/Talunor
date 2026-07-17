# Leçon 04 — Provider LLM & streaming

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 2 · ~60 min

## Pourquoi cette leçon existe

Jusqu'ici Talunor pouvait se souvenir, mais pas *réfléchir*. Cette couche ajoute le
LLM — et le fait d'une manière qui mérite d'être étudiée : l'agent ne parle jamais
directement à Ollama (ou OpenRouter). Il parle à une minuscule **interface**. Cette
seule décision est ce qui rend tout le système testable. Tu la liras à **`v0.3.0`**,
où la couche LLM est toute neuve et sans encombrement.

## Objectifs pédagogiques

À la fin tu sais :
- expliquer pourquoi l'agent dépend d'une *interface* (`llm.Provider`) plutôt que d'un
  client concret ;
- décrire comment une réponse est **streamée** via un channel Go ;
- écrire un faux provider qui renvoie une réponse figée (l'astuce derrière les tests
  déterministes de Talunor).

## Prérequis

- Leçons 00–02. Un peu d'aisance avec les **interfaces** et les **channels** Go ; si
  c'est nouveau, la première heure de [A Tour of Go](https://go.dev/tour) suffit.

## Checkout de la couche LLM

```bash
git checkout v0.3.0     # detached HEAD — lecture seule (voir Leçon 00)
```

> **Fichiers à ce tag** (la mémoire de la Leçon 02, plus une couche LLM) :
>
> ```text
> internal/llm/llm.go        l'interface Provider + les types Message / Chunk / Options
> internal/llm/openai.go     l'unique adaptateur concret (compatible OpenAI : Ollama, OpenRouter)
> internal/llm/openai_test.go  le teste contre un faux serveur HTTP — pas de vrai modèle
> cmd/chat/main.go           un petit programme qui streame un prompt vers un modèle
> internal/memory/…          (inchangé depuis la Leçon 02)
> ```
>
> Toujours pas d'agent — il arrive à `v0.4.0` (Leçon 05).

## Lis, dans cet ordre

```text
internal/llm/llm.go          # le contrat — petit exprès
internal/llm/openai.go       # l'implémentation — streaming SSE
internal/llm/openai_test.go  # comment on le teste sans modèle en direct
```

## Le contrat est minuscule

Voici toute l'interface (à `v0.3.0`) :

```go
type Provider interface {
    // Name identifies the provider (e.g. "ollama") for logs and errors.
    Name() string
    // Chat starts a streaming completion. Setup failures (bad request, connection
    // refused, non-200) are returned as the error; failures mid-stream arrive as a
    // Chunk with Err set. The channel closes when the completion ends or ctx is cancelled.
    Chat(ctx context.Context, msgs []Message, opts Options) (<-chan Chunk, error)
}
```

Deux méthodes. C'est *toute* la dépendance que le reste de Talunor a envers « le LLM ».
`Chat` renvoie un **channel de `Chunk`s** — la réponse arrive morceau par morceau, pour
que l'UI puisse l'afficher en direct au lieu d'attendre l'ensemble. Un `Chunk` porte
`Content` (et `Reasoning`, pour les modèles « qui réfléchissent ») ; un `Chunk.Err`
non-nil est le dernier du channel.

## Pourquoi une interface ? (tout l'intérêt)

Parce que l'agent dépend de `Provider`, pas d'un client Ollama concret, tu peux lui
passer *n'importe quoi* qui satisfait ces deux méthodes — y compris un faux. Ça
apporte :

- **la testabilité** — remplacer le vrai modèle par un double déterministe (pas de
  réseau, pas de coût, pas de « le modèle s'est senti créatif aujourd'hui ») ;
- **l'interchangeabilité** — Ollama, OpenRouter, ou un nouveau provider, derrière une
  seule couture ;
- **le découplage** — la logique de l'agent ignore quel modèle répond, et s'en fiche.

Lis `openai_test.go` : il teste le *vrai* adaptateur contre un faux serveur **HTTP**
(`httptest`) — même idée, un cran plus bas.

## Écris un faux provider

Voici un faux complet et compilable (c'est exactement la forme qu'utilisent les tests
de Talunor). Lis-le et assure-toi de comprendre chaque ligne :

```go
type FixedProvider struct{}

func (FixedProvider) Name() string { return "fixed" }

func (FixedProvider) Chat(ctx context.Context, msgs []llm.Message, opts llm.Options) (<-chan llm.Chunk, error) {
    out := make(chan llm.Chunk, 1)
    out <- llm.Chunk{Content: "Hello from the fake provider"}
    close(out)                 // un chunk, puis le channel se ferme = réponse terminée
    return out, nil            // erreur nil = setup réussi
}
```

Remarque qu'il ne touche jamais au réseau. Tout ce qui détient un `Provider` — y
compris l'agent que tu rencontreras en Leçon 05 — ne peut pas le distinguer du vrai.
*C'est* ainsi qu'on teste un agent IA sans vraie IA.

## Expérience (optionnelle — nécessite Ollama)

Si tu as Ollama qui tourne, streame une vraie réponse :

```bash
make chat PROMPT="explain vector search in one sentence"
```

Regarde les mots apparaître progressivement — c'est le channel de `Chunk`s qui se vide
en temps réel. Pas d'Ollama ? Saute-la ; tu as déjà lu le plus important (le test, et
le faux).

Reviens au code le plus récent une fois fini :

```bash
git switch main
```

## Questions auxquelles répondre

- Quelles sont les *seules* deux choses dont le reste de Talunor a besoin de la part de
  « le LLM » ?
- Comment une réponse streamée arrive-t-elle, et pourquoi le streaming est-il meilleur
  que d'attendre le texte complet ?
- Les erreurs de setup et les erreurs en cours de stream sont gérées différemment — où
  va chacune (l'`error` renvoyée vs un `Chunk.Err`) ? Pourquoi les séparer ?

## Erreurs fréquentes

- **Se tromper de signature.** `Chat` renvoie `(<-chan llm.Chunk, error)` et le provider
  doit aussi implémenter `Name()`. Un faux qui oublie l'un ou l'autre ne satisfait pas
  l'interface — le compilateur te le dira.
- **Oublier de `close` le channel.** Un lecteur parcourt le channel ; si tu ne le fermes
  jamais, le lecteur bloque pour toujours.

## Checklist de complétion

- [ ] Je peux énoncer de mémoire les deux méthodes de `llm.Provider`.
- [ ] Je peux expliquer pourquoi dépendre de l'interface rend l'agent testable.
- [ ] Je comprends comment une réponse est streamée via un channel.
- [ ] J'ai lu `FixedProvider` et je peux expliquer pourquoi il est indiscernable d'un
      vrai provider pour son appelant.
- [ ] Je suis revenu à `main`.

**Suivant :** [Leçon 05 — Suivre la boucle de l'agent](../05-follow-the-agent-loop/) —
maintenant que tu as rencontré la mémoire *et* le modèle, regarde-les se combiner en un
seul tour.
