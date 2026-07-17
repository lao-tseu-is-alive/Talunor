# Leçon 07 — Tester sans vrai LLM

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🛠️ Contribution actuelle** · Niveau 2–3 · ~75 min

> Une leçon de **contribution** : travaille sur `main`, sur ta propre branche.

## Pourquoi cette leçon existe

Comment tester un agent quand le « cerveau » est un grand modèle de langage qui donne
une réponse légèrement différente à chaque fois ? Tu **n'appelles pas le vrai modèle.**
Tu donnes à l'agent un faux *scripté* qui renvoie exactement les chunks que tu choisis.
Cette leçon montre les astuces de test de Talunor — et tu en écriras une toi-même.

## Objectifs pédagogiques

À la fin tu sais :
- expliquer pourquoi un test d'agent ne doit pas dépendre d'un modèle en direct ;
- utiliser un **provider scripté** pour piloter une séquence d'appels d'outils
  déterministe ;
- écrire un test comportemental qui vérifie *ce que l'agent a fait*, pas des détails
  d'implémentation.

## Prérequis

- Leçon 04 (l'interface `Provider` + l'idée d'un faux provider).
- La Leçon 06 aide — tu testeras l'outil que tu y as construit.

## Démarre une branche

```bash
git switch main
git pull
git switch -c learning/agent-tests
```

## Lis la boîte à outils de test

```text
internal/agent/agent_test.go   # fakeProvider, scriptedProvider, fakeTool — les doubles
internal/llm/openai_test.go    # teste le vrai adaptateur contre un serveur SSE httptest
internal/tui/tui_test.go       # pilote la TUI sans terminal (tea.Msg synthétiques)
```

Trois niveaux de la même idée — *remplacer la chose non déterministe par un double
déterministe* :

- les tests **agent** remplacent le LLM par un `scriptedProvider` ;
- les tests **llm** remplacent le réseau par un serveur `httptest` ;
- les tests **tui** remplacent le terminal par des messages synthétiques et vérifient
  `View()`.

## Comment fonctionne un provider scripté

`scriptedProvider` renvoie une réponse figée par appel à `Chat`. Pour tester une boucle
d'outils, tu scriptes deux étapes : *« appelle cet outil »*, puis *« voici la réponse
finale »* :

```go
prov := &scriptedProvider{steps: [][]llm.Chunk{
    // Turn 1: the model asks for a tool (a terminal tool-call chunk).
    {{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "calculator", Args: `{"expression":"2+2"}`}}}},
    // Turn 2: with the observation in context, it answers.
    {{Content: "It's 4."}},
}}
```

L'agent exécute l'outil entre les deux appels, réinjecte le résultat, et la seconde
réponse scriptée devient la réponse — le tout **sans réseau et sans vrai modèle**. Vois
`TestReActToolLoop` dans `agent_test.go` pour le motif complet.

## L'exercice

Écris un test qui pilote **l'outil `unit_convert` que tu as construit en Leçon 06** à
travers l'agent, de façon déterministe. Dans `internal/agent/agent_test.go` (ou un
nouveau `*_test.go`), modèle-le sur `TestReActToolLoop` :

1. Construis un `scriptedProvider` dont la première étape demande `unit_convert`
   (`Args: '{"value":5,"from":"km"}'`), et dont la seconde est une phrase finale.
2. Enregistre l'outil : `cfg.Tools = tools.NewRegistry(tools.UnitConvert{})`.
3. Désactive la réflexion (`cfg.Extractor = DisableReflection()`) pour que le nombre
   d'appels soit exact.
4. Lance un tour, draine le stream, et vérifie :
   - la réponse finale est bien passée ;
   - l'**observation de l'outil** (la valeur en miles) a atteint le modèle — cherche
     dans `prov.lastMsgs` un message `RoleTool` qui la contient.

Lance-le :

```bash
go test ./internal/agent/ -run UnitConvert -v
```

## Le point central

> Un bon test d'agent verrouille le **comportement**, pas l'humeur du modèle. « Étant
> donné ce modèle scripté et cet outil, l'agent exécute l'outil et réinjecte le
> résultat » est un fait que tu peux vérifier à chaque fois — parce que tu contrôles les
> deux bouts.

## Erreurs fréquentes

- **Vérifier le libellé exact.** Ne vérifie pas que le modèle « a dit X » — c'est toi
  qui l'as scripté. Vérifie la *structure* : l'outil a tourné, l'observation est
  revenue, le tour a été stocké.
- **Laisser la réflexion activée.** Elle fait un appel modèle supplémentaire, faussant un
  `scriptedProvider` au nombre d'étapes fixe. Désactive-la dans le test.

## Checklist de complétion

- [ ] Je peux expliquer pourquoi les tests d'agent utilisent un provider scripté plutôt
      qu'un vrai LLM.
- [ ] J'ai écrit un test qui pilote un appel d'outil → observation → réponse finale.
- [ ] Mon test vérifie que l'observation a atteint le modèle (comportement, pas libellé).
- [ ] Le test passe et n'a pas besoin de réseau.

**Suivant :** [Leçon 08 — Observabilité & gestion d'erreurs](../08-observability-and-errors/).
