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
grep -n "_, _ = a.store.Remember" internal/agent/agent.go
```

Tu trouveras quelque chose comme :

```go
_, _ = a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer)
```

Le `_, _ =` jette **les deux** valeurs de retour, y compris l'erreur. C'est un choix
*délibéré* : l'utilisateur a déjà reçu sa réponse, et un accroc de stockage ne devrait
pas la lui retirer. Cette partie est correcte. **Mais l'erreur est aussi invisible** —
si le stockage du tour assistant échoue de façon répétée, la mémoire long terme devient
silencieusement asymétrique (la question sauvée, la réponse non) et personne ne sait
pourquoi.

> *(Si, au moment où tu lis ceci, cette ligne a déjà été durcie — parfait, c'est cette
> leçon qui atterrit dans le vrai projet. Étudie le diff à la place.)*

## Lis comment l'observabilité marche déjà

Talunor a une trace légère, désactivée par défaut. Lis :

```text
internal/agent/agent.go     # le helper a.trace("…", …) et ses points d'appel
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

**Suivant :** [Leçon 09 — Récupération web sécurisée (SSRF)](../09-secure-web-fetching/),
une leçon de sécurité **avancée**.
