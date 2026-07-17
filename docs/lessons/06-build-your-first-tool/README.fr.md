# Leçon 06 — Construire ton premier outil

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🛠️ Contribution actuelle** · Niveau 2 · ~90 min

> C'est une leçon de **contribution** : tu modifies le projet *actuel*. Travaille sur
> `main`, sur ta propre branche — pas sur un tag détaché.

## Pourquoi cette leçon existe

Tu as lu comment Talunor fonctionne ; maintenant tu vas y *ajouter*. Le meilleur
premier changement est un nouvel **outil** — une capacité que l'agent peut appeler —
parce que tu peux en ajouter un **sans toucher au cœur de l'agent**. C'est tout
l'intérêt de l'interface d'outil, et le faire une fois t'apprend à quel point un bon
point d'extension est agréable.

## Objectifs pédagogiques

À la fin tu sais :
- implémenter l'interface `tools.Tool` de Talunor de zéro ;
- enregistrer un outil pour que l'agent puisse l'appeler ;
- écrire des tests table-driven pour lui ;
- expliquer pourquoi ajouter une capacité par *extension* vaut mieux que modifier
  l'orchestrateur.

## Prérequis

- Leçons 00–05. Tu as vu la boucle de l'agent appeler des outils (Leçon 05).

## Démarre une branche (sur `main`)

```bash
git switch main
git pull
git switch -c learning/unit-convert-tool
```

## Lis ton modèle

```text
internal/tools/tool.go       # l'interface Tool + le Registry
internal/tools/builtin.go    # Calculator et Clock — copie leur forme
internal/tools/tools_test.go # comment les builtins sont testés
```

Tout le contrat tient en quatre méthodes :

```go
type Tool interface {
    Name() string                 // id stable que le modèle appelle, snake_case
    Description() string           // ce qu'il fait / quand l'utiliser (le modèle le lit)
    Schema() json.RawMessage       // JSON Schema pour les arguments (un "object")
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}
```

`Execute` renvoie la chaîne que le modèle va *observer*. Une `error` renvoyée n'est pas
fatale — elle est remise au modèle comme observation pour qu'il puisse se rattraper.

## L'exercice — un outil `unit_convert`

Ajoute un outil qui convertit entre quelques unités :

- kilomètres → miles
- Celsius → Fahrenheit
- kilogrammes → livres

Crée `internal/tools/unitconvert.go`. Voici le squelette — remplis les `// TODO`
(utilise `Calculator` dans `builtin.go` comme référence) :

```go
package tools

import (
    "context"
    "encoding/json"
    "fmt"
)

type UnitConvert struct{}

func (UnitConvert) Name() string { return "unit_convert" }

func (UnitConvert) Description() string {
    return "Convert a value between units. Supported: km→mi, c→f, kg→lb."
}

func (UnitConvert) Schema() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "value": { "type": "number", "description": "the amount to convert" },
            "from":  { "type": "string", "description": "source unit: km, c, or kg" }
        },
        "required": ["value", "from"]
    }`)
}

func (UnitConvert) Execute(_ context.Context, args json.RawMessage) (string, error) {
    var in struct {
        Value float64 `json:"value"`
        From  string  `json:"from"`
    }
    if err := json.Unmarshal(args, &in); err != nil {
        return "", fmt.Errorf("invalid arguments: %w", err)
    }
    switch in.From {
    case "km":
        return fmt.Sprintf("%.6g mi", in.Value*0.621371), nil
    // TODO: "c"  -> Fahrenheit:  value*9/5 + 32
    // TODO: "kg" -> pounds:      value*2.2046226
    default:
        return "", fmt.Errorf("unsupported unit %q (use km, c, or kg)", in.From)
    }
}
```

Ensuite **enregistre-le** pour que l'agent se le voie proposer — trouve où les builtins
sont enregistrés dans `cmd/talunor/main.go` (cherche `tools.NewRegistry`) et ajoute
`tools.UnitConvert{}` à la liste.

## Écris des tests-table

Crée `internal/tools/unitconvert_test.go`. Un test-table fait passer plusieurs cas dans
une seule boucle (vois `tools_test.go` pour le motif). Couvre au moins :

```text
1 km   → "0.621371 mi"
0 c    → "32 f"          (ou comme tu formates)
unité invalide  → erreur
valeur manquante → erreur (ou un défaut documenté)
```

Lance-les :

```bash
go test ./internal/tools/ -run UnitConvert -v
```

## Essaie de bout en bout (optionnel, nécessite Ollama)

```bash
TALUNOR_TOOLS=1 go run ./cmd/talunor --plain
# puis demande : "how far is 5 km in miles?"
```

## Le principe

> Ajouter une capacité par **extension** (un nouveau `Tool`) est plus sûr que de
> **modifier** l'orchestrateur. La boucle de l'agent n'a jamais changé — tu as
> seulement ajouté quelque chose qu'elle peut choisir d'appeler. Une bonne architecture
> fait du changement *courant* (une nouvelle capacité) le changement *facile*.

## Erreurs fréquentes

- **Une `Description` vague.** Le modèle décide d'appeler ton outil à partir de ce
  texte — sois concret sur ce qu'il fait et quand.
- **Ne pas valider les arguments.** Renvoie une `error` claire pour une entrée
  invalide ; le modèle la verra et pourra se corriger.
- **Oublier d'enregistrer l'outil.** S'il n'est pas dans le registry, l'agent ne le voit
  jamais.

## Checklist de complétion

- [ ] J'ai implémenté les quatre méthodes de `Tool`.
- [ ] J'ai enregistré `unit_convert` dans `cmd/talunor/main.go`.
- [ ] J'ai écrit des tests-table, dont au moins un cas d'erreur, et ils passent.
- [ ] Je peux expliquer pourquoi ça n'a pas nécessité de changer la boucle de l'agent.
- [ ] Mon travail est sur une branche `learning/…`, pas directement sur `main`.

**Suivant :** [Leçon 07 — Tester sans vrai LLM](../07-test-without-a-real-llm/).
