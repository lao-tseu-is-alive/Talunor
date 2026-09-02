# Leçon 06 — Construire ton premier outil

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🛠️ Contribution actuelle** · Niveau 2 · ~2h

> C'est une leçon de **contribution** : tu modifies le projet *actuel*. Travaille sur
> une branche de **ton propre fork** — pas sur un tag détaché, et pas sur le `main` de
> ce dépôt.

## Pourquoi cette leçon existe

Tu as lu comment Talunor fonctionne ; maintenant tu vas y *ajouter*. Le meilleur
premier changement est un nouvel **outil** — une capacité que l'agent peut appeler —
parce que tu peux en ajouter un **sans toucher au cœur de l'agent**. C'est tout
l'intérêt de l'interface d'outil, et le faire une fois t'apprend à quel point un bon
point d'extension est agréable.

Tu vas aussi rencontrer, en trois lignes de Go, la distinction dont parle tout le reste
du cours : **ce que ton logiciel garantit** face à **ce que tu demandes simplement au
modèle de faire**.

## Objectifs pédagogiques

À la fin tu sais :
- implémenter l'interface `tools.Tool` de Talunor de zéro ;
- expliquer pourquoi un JSON Schema contraint le *modèle* et ne valide *rien* en Go ;
- enregistrer un outil pour que l'agent puisse l'appeler — et dire ce que
  l'enregistrement garantit et ne garantit pas ;
- écrire des tests-table pour lui ;
- expliquer pourquoi ajouter une capacité par *extension* vaut mieux que modifier
  l'orchestrateur ;
- ouvrir une pull request qu'un mainteneur peut relire sans te poser une seule
  question.

## Prérequis

- Leçons 00–05. Tu as vu la boucle de l'agent appeler des outils (Leçon 05).
- `gh` (GitHub CLI) si tu veux ouvrir la PR depuis le terminal.

## D'abord un fork, ensuite une branche

Cette leçon se termine par une vraie pull request, donc pars d'un fork :

```bash
gh repo fork lao-tseu-is-alive/Talunor --clone --remote   # la première fois
# ou, depuis un clone que tu as déjà :
git remote add fork git@github.com:<ton-user>/Talunor.git

git switch main
git pull
git switch -c learning/unit-convert-tool
```

Pourquoi un fork : la PR que tu ouvres à la fin vise **ton** dépôt. La section
[Proposer ton patch](#proposer-ton-patch) explique pourquoi — lis-la avant de pousser.

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
fatale — `Registry.Execute` la transforme en observation préfixée par `error:` et la
remet au modèle pour qu'il puisse se rattraper.

## L'exercice — un outil `unit_convert`

Ajoute un outil qui convertit quelques unités :

- kilomètres → miles
- Celsius → Fahrenheit
- kilogrammes → livres

### Étape 1 — prédis, avant d'écrire quoi que ce soit

Voici le squelette. **Il contient un défaut délibéré** — c'est l'exercice, pas un bug à
signaler. Lis-le, puis réponds à la question ci-dessous *avant* de lancer quoi que ce
soit.

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
    // TODO: "c"  -> Fahrenheit :  value*9/5 + 32
    // TODO: "kg" -> livres :      value*2.2046226
    default:
        return "", fmt.Errorf("unsupported unit %q (use km, c, or kg)", in.From)
    }
}
```

> **La question.** Le schéma dit `"required": ["value", "from"]`. Le modèle envoie
> `{"from":"c"}` — JSON valide, sans `value`. Que renvoie `Execute` : une erreur, ou un
> résultat ? Écris ta réponse avant de lire la suite.

### Étape 2 — remplis les TODO, puis teste ta prédiction

Crée `internal/tools/unitconvert.go` avec le squelette ci-dessus et complète les deux
`TODO`. Puis vérifie ta prédiction avec un test jetable :

```go
func TestPrediction(t *testing.T) {
    got, err := tools.UnitConvert{}.Execute(context.Background(),
        json.RawMessage(`{"from":"c"}`))
    t.Log(got, err)
}
```

```bash
go test ./internal/tools/ -run Prediction -v
```

`json.Unmarshal` réussit. `Value` reste à la zero value de `float64`, donc l'outil
répond **`32 f`** — une conversion sûre d'elle, bien formatée, et entièrement inventée,
d'une température que personne n'a envoyée.

`required` est une ligne dans un document que tu tends au modèle. `encoding/json` n'a
jamais entendu parler de ton schéma. **Rien dans le système de types de Go ne distingue
« le champ était absent » de « le champ valait zéro »** — et ici zéro est une entrée
parfaitement légitime, puisque 0 °C est une vraie température.

### Étape 3 — représenter l'absence

Quel type Go peut contenir *à la fois* un `float64` et le fait qu'aucun `float64` n'a
été fourni ? Change une ligne :

```go
Value *float64 `json:"value"`
```

Les états deviennent distincts :

```text
champ absent       → nil
"value": null      → nil
"value": 0         → pointeur vers 0
mauvais type JSON  → erreur de décodage
```

Valide après le décodage, avant la conversion :

```go
if in.Value == nil {
    return "", errors.New("missing required argument \"value\"")
}
```

…et déréférence (`*in.Value`) là où tu fais le calcul — le compilateur te le rappellera.

Fais le même raisonnement pour `From`, et remarque qu'il n'a besoin d'*aucun* pointeur :
la zero value d'un `string` est `""`, qui n'est pas une unité supportée, donc elle tombe
dans ta branche `default` et échoue d'elle-même. **Prends un pointeur quand la zero value
est une entrée valide, pas par réflexe.**

## Ce que Go garantit, ce que le modèle décide

Voici le tableau à retenir, et la raison pour laquelle cette leçon précède les leçons de
sécurité :

```text
Go garantit                            Le modèle décide
-------------------------------------  -------------------------------------
la présence dans le Registry           s'il appelle l'outil, ou pas
le routage exact, par Name             quels arguments il invente
la validation que tu as écrite         comment il lit ta Description
le calcul de conversion                comment il interprète ton observation
la réinjection de l'observation        s'il se rattrape après ton erreur
```

`Name`, `Description` et `Schema` sont un **contrat proposé** au modèle. `Execute` est
le **seul contrat imposé**. Un schéma qui dit `required` et un `Execute` qui suppose
qu'il a été respecté, c'est une garantie sur le qualificatif, pas sur l'entrée — tu
retrouveras cette forme exacte à la Leçon 14, un étage plus bas, où une approbation
liait le *nom* d'un outil mais pas ses *arguments*.

## Écris des tests-table

Crée `internal/tools/unitconvert_test.go`. Un test-table fait passer plusieurs cas dans
une seule boucle (vois `tools_test.go` pour le motif). Couvre au moins :

```text
1 km    → "0.621371 mi"
0 c     → "32 f"        ← prouve que 0 est accepté…
value absente → erreur  ← …pendant que l'absence est refusée. Les deux, ou aucun ne prouve rien.
-40 c   → "-40 f"
unité inconnue → erreur
JSON mal formé / mauvais type → erreur
```

Deux conventions à copier :

- **Utilise `package tools_test`.** Ce package est honnêtement mixte —
  `tools_test.go` est externe, `bash_test.go` et `webfetch_test.go` sont internes — donc
  c'est un choix raisonné, pas une règle maison : ton outil n'expose rien de non exporté,
  teste-le donc par la même porte que l'agent. (Vérifie toi-même :
  `head -3 internal/tools/*_test.go`. Vérifier une affirmation de ce genre est tout le
  sujet de la Leçon 15.)
- **Assertionne `wantErr bool`, pas le texte de l'erreur.** Le message ne fait pas partie
  du contrat et sera reformulé. Si la *nature* de l'échec devient un jour contractuelle,
  c'est à ça que servent une erreur sentinelle et `errors.Is`.

```bash
go test ./internal/tools/ -run UnitConvert -v
```

## Enregistre-le — une garantie séparée

Des tests verts prouvent que tes conversions sont justes. Ils ne prouvent rien sur la
capacité de l'agent à *appeler* ton outil : un outil jamais enregistré est du code mort,
et tous tes tests passent quand même.

Trouve où les builtins sont enregistrés dans `cmd/talunor/main.go` (cherche
`tools.NewRegistry`) et ajoute `tools.UnitConvert{}` :

```go
reg := tools.NewRegistry(
    tools.Calculator{},
    tools.Clock{},
    tools.UnitConvert{},
    tools.NewRecallMemory(store),
)
```

Cette ligne garantit que l'outil peut être *annoncé et routé*. Elle ne garantit pas que
le modèle le choisira.

## Essaie de bout en bout (optionnel, nécessite Ollama)

```bash
TALUNOR_TOOLS=1 go run ./cmd/talunor --plain
# puis demande : "how far is 5 km in miles?"
```

```text
🔧 unit_convert({"value":5,"from":"km"})
   ↳ 3.10685 mi
```

Suis le chemin complet une fois : `Registry.Defs()` → `Agent.toolSpecs()` →
`opts.Tools` → le modèle émet un `ToolCall` → `reactLoop` → `runTool` →
`policy.Evaluate` → `Registry.Execute` → ton `Execute` → une observation avec `RoleTool`
→ un nouvel appel au provider. Déterministe partout, sauf aux deux extrémités.

## Le principe

> Ajouter une capacité par **extension** (un nouveau `Tool`) est plus sûr que de
> **modifier** l'orchestrateur. La boucle de l'agent n'a jamais changé — tu as
> seulement ajouté quelque chose qu'elle peut choisir d'appeler. Une bonne architecture
> fait du changement *courant* (une nouvelle capacité) le changement *facile*.

Regarde ce que ta branche touche réellement : un fichier neuf, un fichier de test, une
ligne de composition. C'est *ça* la mesure, et c'est pour ça que l'interface existe —
non pas parce que les interfaces sont bien, mais parce que ce diff est petit.

## Proposer ton patch

Tu as maintenant du code qui marche et aucune idée de quoi en faire. C'est dans cet écart
que meurent la plupart des premières contributions, donc cette partie fait partie de la
leçon.

### Où va cette PR — et où elle ne va pas

**Ouvre la pull request contre ton propre fork** (`ton-user/Talunor`, branche → `main`).
C'est une vraie PR : vrai diff, vraie description, vraie relecture, vraie fusion.

```bash
git push -u fork learning/unit-convert-tool
gh pr create --repo <ton-user>/Talunor --base main --head learning/unit-convert-tool
```

> `gh pr create` vise le dépôt **amont** par défaut quand il détecte un fork. Le `--repo`
> explicite n'est pas décoratif.

**N'envoie pas `unit_convert` en amont.** Chaque lecteur de cette leçon écrit le même
outil ; la centième PR identique coûte du temps réel à un mainteneur et n'apporte rien
que le projet veuille. Ce n'est pas de la fermeture, c'est la première règle de la
contribution :

> Un patch se juge à ce qu'il change pour le mainteneur, pas à ce qu'il t'a coûté.

### Relis-toi avant de demander une relecture

Lis ton propre diff avant tout le monde — `git diff main...HEAD`, en entier, à voix haute
si ça aide. Puis lance la porte que ce projet utilise vraiment :

```bash
make release-check
```

Attends-toi à un **échec**, et attends-toi à ce qu'il soit intéressant :

```text
atlas: not referenced: internal/tools/unitconvert.go
docs/atlas.md is stale — regenerate it
```

`atlas-check` parcourt `git ls-files` et exige que chaque fichier suivi apparaisse dans
`docs/atlas.md`. Les fichiers non suivis passent ; à la seconde où tu commites, non.
Rien n'a été cassé par ton code — une **alarme de dérive** a remarqué que tu avais ajouté
un fichier au projet et pas à la carte du projet. Régénère l'atlas (la skill `repo-atlas`,
ou ajoute les deux lignes à la main) et relance.

Cet échec est la chose la plus transférable de cette leçon : un dépôt mûr te dit ce que tu
as oublié, au prix d'une commande, *avant* qu'un humain ait à le faire.

Puis écris le commit et le corps de la PR :

- **Conventional Commits**, comme le reste de l'historique :
  `feat(tools): add a unit_convert tool (km→mi, c→f, kg→lb)`.
- **Le corps de la PR répond à trois questions** : ce qui change, pourquoi c'est
  souhaitable, comment tu sais que ça marche. Deux phrases et la commande de test
  suffisent. Si tu ne peux pas dire pourquoi c'est souhaitable, c'est une information sur
  le patch, pas sur ton écriture.
- **Nomme ce que tu n'as pas fait.** « Trois unités seulement ; pas de sens
  impérial→métrique. » Un relecteur fait davantage confiance à un diff dont la limite est
  annoncée qu'à un diff qui laisse croire qu'il est complet.

### Ce que ce dépôt veut réellement

Si tu veux contribuer ici pour de vrai, le travail ouvert est listé dans `AGENTS.md`,
sous **« Next — open threads »** — findings, cibles de fuzzing, benchmarks, couverture
manquante. La barre est écrite dans le même fichier, sous *« How it is built »* :
`make release-check` vert, `docs/atlas.md` régénéré quand des fichiers bougent, et — si
ton changement est un *layer* — une leçon bilingue dans `docs/lessons/`, parce qu'une
fonctionnalité sans sa leçon n'est que la moitié de ce que ce projet livre.

Commence par ouvrir une issue qui énonce le problème avant d'écrire le correctif. Le
patch le moins cher à relire est celui dont le problème a d'abord été admis.

## Erreurs fréquentes

- **Faire confiance au schéma.** `required` est une demande au modèle. Valide dans
  `Execute`, ou tu ne valides nulle part.
- **Un pointeur partout.** Seulement là où la zero value est une entrée légale. `From`
  n'en a pas besoin.
- **Une `Description` vague.** Le modèle décide d'appeler ton outil à partir de ce
  texte — sois concret sur ce qu'il fait et quand.
- **Oublier d'enregistrer l'outil.** S'il n'est pas dans le registry, l'agent ne le voit
  jamais — et tous les tests passent quand même.
- **Assertionner le texte des erreurs.** Il sera reformulé ; ta suite rougira pour rien.
- **Ouvrir la PR contre l'amont.** Voir plus haut ; vérifie le flag `--repo`.

## Checklist de complétion

- [ ] J'ai prédit ce que ferait `{"from":"c"}`, et j'ai su expliquer *pourquoi* la
      réponse était `32 f` avant de changer quoi que ce soit.
- [ ] J'ai implémenté les quatre méthodes de `Tool`, avec validation dans `Execute`.
- [ ] Je peux citer une chose que `Schema()` garantit et une chose qu'il ne garantit pas.
- [ ] J'ai enregistré `unit_convert` dans `cmd/talunor/main.go`.
- [ ] Mes tests-table incluent **à la fois** `0 c` et une `value` manquante.
- [ ] J'ai lancé `make release-check`, compris pourquoi `atlas-check` échouait, et corrigé.
- [ ] J'ai ouvert une PR **contre mon propre fork**, avec un corps qu'un inconnu pourrait
      relire.
- [ ] Je peux expliquer pourquoi ça n'a pas nécessité de changer la boucle de l'agent.

**Suivant :** [Leçon 07 — Tester sans vrai LLM](../07-test-without-a-real-llm/README.fr.md).
