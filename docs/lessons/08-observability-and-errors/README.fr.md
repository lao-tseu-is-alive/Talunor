# Leçon 08 — Observabilité & gestion d'erreurs

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🛠️ Contribution actuelle** · Niveau 2 · ~45 min

> Une leçon de **contribution** : travaille sur `main`, sur ta propre branche.

## Pourquoi cette leçon existe

Toute erreur ne doit pas faire planter le programme — mais aucune erreur ne devrait
disparaître *silencieusement*. Cette leçon étudie un exemple réel et vivant dans
Talunor où une erreur est délibérément ignorée, demande si c'est correct, et te fait
rendre l'échec **visible** sans casser l'expérience de l'utilisateur.

## Objectifs pédagogiques

À la fin tu sais :
- distinguer les erreurs *fatales*, *récupérables* et *best-effort* ;
- transformer un échec silencieux en un échec observable via la trace de l'agent ;
- expliquer ce qui a sa place dans un log de debug — et ce qui n'y en a jamais.

## Prérequis

- Leçon 05 (la boucle de l'agent). La Leçon 04 aide.

## Démarre une branche

```bash
git switch main
git pull
git switch -c learning/observability
```

## Le cas réel

Après un tour, l'agent stocke la réponse de l'assistant. Trouve l'appel — cherche-le
dans l'agent :

```bash
git show v0.13.2:internal/agent/agent.go | grep -n "_, _ = a.store.Remember"
```

Remarque le tag épinglé. Sur le `main` d'aujourd'hui, ce grep ne trouve **rien** — la
ligne a été durcie en `v0.13.3`, et cette leçon montre à quoi ressemble une affirmation
de cours qui survit au code qu'elle décrit (la règle de la leçon 15 : *une affirmation
est vraie face à un commit*). Lis l'original à `v0.13.2`, raisonne dessus, puis étudie
le correctif à la fin.

À ce tag, tu trouveras :

```go
_, _ = a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer)
```

Le `_, _ =` jette **les deux** valeurs de retour, y compris l'erreur. C'est un choix
*délibéré* : l'utilisateur a déjà reçu sa réponse, et un accroc de stockage ne devrait
pas la lui retirer. Cette partie est correcte. **Mais l'erreur est aussi invisible** —
si le stockage du tour assistant échoue de façon répétée, la mémoire long terme devient
silencieusement asymétrique (la question sauvée, la réponse non) et personne ne sait
pourquoi.

Cette ligne **a** depuis été durcie — cette leçon a atterri dans le vrai projet. Vois
exactement comment, et remarque que le choix délibéré « ne pas retirer la réponse » a
survécu :

```bash
git diff v0.13.2 v0.13.3 -- internal/agent/agent.go | grep -A6 "store.assistant.error"
```

Sur le `main` actuel, ce code vit dans `internal/agent/turn.go` (la boucle a été
extraite d'`agent.go` en `v0.22.5`).

## Lis comment l'observabilité marche déjà

Talunor a une trace légère, désactivée par défaut. Lis :

```text
internal/agent/agent.go     # le helper a.trace("…", …) lui-même
# ses points d'appel sont répartis dans le paquet : grep -rn 'a.trace(' internal/agent/
cmd/talunor/main.go         # debugLogger — comment TALUNOR_DEBUG est câblé
```

`a.trace(...)` ne fait rien sauf si `TALUNOR_DEBUG` est défini, donc l'instrumentation
est gratuite quand elle est désactivée. Vois-la en action :

```bash
TALUNOR_DEBUG=stderr go run ./cmd/talunor --plain    # (nécessite Ollama pour un tour complet)
```

## L'exercice

Rends l'échec de stockage silencieux observable — sans changer le comportement « ne pas
retirer la réponse ». Remplace l'erreur jetée par une trace :

```go
if _, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer); err != nil {
    a.trace("store.assistant.error", "err", err)
}
```

La réponse est toujours renvoyée ; le tour se termine toujours ; mais maintenant un
échec laisse une trace que tu peux retrouver avec `TALUNOR_DEBUG`. Lance la suite pour
confirmer que rien n'est cassé :

```bash
go test ./internal/agent/ -count=1
```

## Le principe

```text
Erreur non-bloquante   ≠   erreur invisible.
```

Ignorer une erreur est acceptable **seulement** quand la décision est explicite *et*
observable. `_, _ =` n'est ni évident ni observable ; une trace le rend les deux.

## Ce qui ne doit jamais aller dans un log

La trace de debug de Talunor peut inclure des extraits de mémoire rappelée, donc elle
est **opt-in et locale** pour une raison. Quand tu ajoutes de l'observabilité :

- **Ne logge jamais** de secrets, de clés d'API ou de contenu utilisateur complet par
  défaut.
- Logge des *identifiants et des formes* (ids, compteurs, distances, types d'erreur),
  pas des données personnelles brutes.

## Pour aller plus loin (avancé)

Tester cet échec correctement signifie injecter un store qui *échoue à la demande* —
mais l'`Agent` dépend actuellement du `*memory.Store` concret, pas d'une interface. Une
façon propre est une petite interface locale :

```go
type memoryStore interface {
    Recall(context.Context, string, int, float64) ([]memory.Hit, error)
    Remember(context.Context, memory.Kind, string, string) (*memory.Memory, error)
}
```

Introduis-la *seulement* si tu ajoutes vraiment le test d'erreur — sinon c'est une
abstraction sans client. (Reconnaître *quand* une interface gagne sa place est en soi
la leçon.)

## Checklist de complétion

- [ ] J'ai trouvé l'appel `_, _ = a.store.Remember(...)` et je peux expliquer pourquoi
      l'erreur a été ignorée — et pourquoi ce n'est quand même pas idéal.
- [ ] Je l'ai remplacé par une version tracée, en gardant la réponse intacte.
- [ ] `go test ./internal/agent/` passe toujours.
- [ ] Je peux nommer deux choses qui ne doivent jamais apparaître dans un log.
- [ ] Je peux expliquer, en une phrase, « non-bloquant ≠ invisible ».

**Suivant :** [Leçon 09 — Récupération web sécurisée (SSRF)](../09-secure-web-fetching/README.fr.md),
une leçon de sécurité **avancée**.
