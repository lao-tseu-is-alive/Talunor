# Leçon 13 — Planifier avant d'agir : du ReAct émergent à un plan qu'on peut lire

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** (lecture du code de `v0.13.0`, avec des exécutions 🛠️
sur `main`) · Niveau 3 (avancé) · ~90 min

## Pourquoi cette leçon existe

Tout ce que l'agent a fait jusqu'ici, il l'a fait de façon *émergente*. Dans la
boucle ReAct (Leçon 05), tu ne découvres ce que Talunor va faire qu'en le regardant
le faire : il appelle un outil, voit le résultat, décide de l'appel suivant, et
ainsi de suite. C'est puissant et adaptatif — mais le plan ne vit que dans la tête
du modèle, une étape en avance sur toi, et la première fois que tu vois l'étape
trois, c'est quand elle se produit.

La Leçon 12 a donné à l'agent un **garde-fou** (la policy). Cette leçon lui donne de
la **préméditation** : un planner optionnel qui écrit tout le plan *d'abord* — une
liste structurée d'étapes que tu peux lire, approuver, ou refuser avant qu'un seul
outil ne tourne. C'est l'aboutissement de l'Itération 3, et il transforme « fais-moi
confiance, je vais me débrouiller » en « voici exactement ce que j'ai l'intention de
faire ».

L'ingénierie intéressante est à deux endroits : obtenir un *plan structuré fiable*
d'un générateur de texte fondamentalement peu fiable, et *exécuter ce plan en
sécurité* sans jeter l'adaptativité qui faisait la force de ReAct.

## Objectifs d'apprentissage

À la fin, tu sais :
- opposer l'exécution **émergente** (ReAct) et **délibérée** (plan d'abord), et dire
  ce que chacune sacrifie ;
- expliquer comment Talunor extrait un **plan JSON valide** d'un LLM — un contrat
  strict, une extraction tolérante, une validation, et un retry qui renvoie l'erreur ;
- suivre le tour planifié — **plan → pré-filtrage policy → approbation du plan entier
  → exécution capée → apprentissage** — et expliquer le *cap structurel* qui le
  sécurise ;
- choisir un mode d'approbation (`plan` / `step` / `highrisk`) et prédire ses prompts ;
- dire pourquoi un échec de planification est une *dégradation*, pas une impasse.

## Prérequis

- **Leçon 05 (la boucle de l'agent)** — tu dois connaître la boucle ReAct sur
  laquelle ceci s'appuie.
- **Leçon 12 (la policy)** — le planner s'appuie sur la policy pour filtrer un plan.
- **Leçon 07 (tester sans vrai LLM)** — les expériences incluent des tests
  déterministes qui planifient et exécutent sans modèle.

## Partie 1 — deux façons d'atteindre un objectif

Demande à Talunor « combien font 12 × 8, utilise la calculatrice ». Deux
architectures y répondent très différemment.

**Émergente (ReAct, par défaut).** On tend les outils au modèle et il se met à
parler : il décide *maintenant* d'appeler `calculator`, voit `96`, puis décide
*maintenant* de répondre. La séquence est découverte au fur et à mesure. Si un
résultat le surprend, il s'adapte sur-le-champ. Mais tu ne peux pas inspecter la
séquence à l'avance — elle n'existe pas encore.

**Délibérée (plan d'abord, `TALUNOR_PLANNER=1`).** On demande d'abord au modèle un
*plan* : un objet JSON listant les étapes. Ce n'est qu'une fois ce plan produit — et
vu par toi — que quoi que ce soit s'exécute. Tu échanges un peu d'adaptativité (le
plan est décidé en amont) contre quelque chose de précieux : les actions deviennent
**inspectables et approuvables en bloc**, avant qu'aucune n'ait lieu.

Aucune n'est « meilleure ». ReAct est agile ; la planification est lisible et
contrôlable. Talunor livre les deux et te laisse basculer avec une seule variable
d'environnement — précisément pour que tu sentes la différence. Le reste de cette
leçon porte sur le fonctionnement de la voie délibérée.

> **L'idée centrale.** L'exécution émergente révèle son plan en agissant.
> L'exécution délibérée énonce son plan, puis agit. La seconde vaut la peine d'être
> construite quand *voir le plan d'abord* — pour l'approuver, le caper, ou le
> refuser — compte plus que s'adapter en vol.

## Partie 2 — extraire un plan d'un LLM (lis `planner.go` à `v0.13.0`)

C'est la couche actuelle. Si `main` a avancé, lis-la telle qu'elle a atterri :

```bash
git checkout v0.13.0        # detached HEAD — lecture seule (voir Leçon 00)
```

Ouvre :

```text
internal/agent/planner.go
```

Un LLM émet du *texte*, pas des structures de données. En tirer un `plan.Plan`
fiable est une discipline en quatre temps, réutilisable dans toute tâche de sortie
structurée :

1. **Un contrat strict.** `planSystemPrompt` dit au modèle de répondre avec *seulement*
   un objet JSON d'une forme précise, liste les outils disponibles, et énonce les
   règles (chaque étape a une rationale ; une étape tool nomme un outil listé ; finir
   par une étape final). Un contrat étroit et vérifiable par la machine est ce qui
   rend la réponse sûre à exécuter — le même instinct que le prompt rigide de
   l'extracteur de faits de la Leçon 05.
2. **Extraction tolérante.** Les modèles enrobent le JSON de prose et de barrières
   ` ```json `. `decodePlan` ne combat pas ça avec une regex fragile : il trouve le
   premier `{` et confie le reste à un `json.Decoder`, qui lit exactement **une**
   valeur JSON et ignore le texte suivant — et gère correctement les accolades dans
   les chaînes. Robuste, en cinq lignes.
3. **Validation au-delà du parsing.** Un JSON valide n'est pas un plan valide.
   `decodePlan` lance `plan.Validate()` (structure, ids uniques, `depends_on`
   résolvable — le paquet `plan` de la Leçon 12), puis ajoute les deux vérifications
   que seul l'agent peut faire : chaque étape tool nomme un outil **connu**, et le
   plan **finit par une étape final**.
4. **Un retry qui enseigne.** Si une vérification échoue, le planner redemande — mais
   il renvoie l'erreur exacte et réaffiche la mauvaise réponse, pour que le modèle
   *corrige* plutôt que *répète*. Un retry (`maxPlanAttempts = 2`) suffit à un modèle
   capable ; plus ne fait que brûler des tokens sur un modèle qui ne peut pas s'y
   plier. Et surtout, le planner **n'exécute jamais d'outil** — il produit seulement
   le plan.

Voilà toute l'histoire de la fiabilité : un prompt serré, une extraction indulgente,
une vraie validation, et un retry auto-correcteur.

## Partie 3 — exécuter un plan en sécurité (lis `execute.go`)

Ouvre :

```text
internal/agent/execute.go
```

Trouve `runPlanned`. C'est le tour planifié, en quatre phases qui reflètent le modèle
de cognition — **plan → gate → exécution → apprentissage** :

1. **Plan.** Demande au planner. S'il échoue, n'abandonne pas le tour : retombe sur la
   boucle ReAct classique pour que l'utilisateur ait quand même une réponse. *La
   planification est une amélioration, pas un point unique de défaillance.*
2. **Pré-filtrage policy.** Évalue chaque étape tool contre la policy (Leçon 12). Une
   seule étape **refusée** bloque le plan *entier* avant que quoi que ce soit ne
   tourne — fail closed, avec une explication.
3. **Approbation du plan entier.** L'humain voit le plan complet — les outils et
   arguments exacts — et l'approuve une fois. C'est le glissement d'UX clé par rapport
   à la gate par-appel de la Leçon 12 : tu consens au *plan*, pas à chaque étape
   isolément.
4. **Exécution capée.** Voici l'astuce porteuse. L'exécution *réutilise la boucle
   ReAct* (`reactLoop`, extraite pour que les voies classique et planifiée partagent
   un seul cœur de confiance) — mais elle n'offre au modèle **que les outils nommés
   par le plan** (`toolSpecs(exec.allowTools)`). Le modèle ne *peut* littéralement pas
   appeler un outil que le plan approuvé n'incluait pas, car il ne le voit jamais.

> **Pourquoi le cap n'est pas optionnel.** Un « oui » global à un plan serait *plus
> faible* que l'approbation par-outil si le modèle pouvait ensuite appeler n'importe
> quoi. La sûreté de l'approbation-plan repose entièrement sur le fait que
> l'exécution reste dans le plan. Talunor l'impose à la surface de l'API — la liste
> d'outils offerte — pas en demandant au modèle de bien se tenir. Impose les
> frontières là où elles ne se discutent pas.

Tu peux voir la scission de la boucle directement :

```bash
git diff v0.12.0 v0.13.0 -- internal/agent/agent.go
```

`runLoop` est devenu un point d'entrée mince au-dessus d'un `reactLoop` partagé, qui
prend désormais un `execCtx` portant le cap d'outils et le fait que l'approbation du
plan entier tient déjà lieu de prompts par-étape.

**Les modes d'approbation** (`TALUNOR_APPROVAL`, défaut `plan`) règlent le
human-in-the-loop :

| mode | prompt plan entier | cap d'outils | prompt par-étape risquée |
|------|--------------------|--------------|--------------------------|
| `plan` | oui, une fois | oui | non (l'approbation-plan est le consentement) |
| `step` | oui, une fois | oui | oui (ceinture et bretelles) |
| `highrisk` | non | non | oui (le plan est advisory ; comme la Leçon 12) |

Le **deny** de la policy est appliqué dans tous les modes. Quand tu as fini de lire,
reviens :

```bash
git switch main
```

## Partie 4 — regarde-le planifier

D'abord la voie déterministe — sans modèle (Leçon 07) :

```bash
go test ./internal/agent/ -run 'Planner|Planned|DecodePlan' -v
```

Lis ces tests à côté du code : un plan valide, un retry-puis-succès, `decodePlan`
tolérant la prose et les fences, et le tour planifié qui approuve / refuse / rejette
un plan et retombe sur ReAct en cas d'échec.

Maintenant en live (nécessite Ollama). Utilise `highrisk` d'abord pour qu'un plan
calculatrice à faible risque tourne sans aucun prompt :

```bash
TALUNOR_PLANNER=1 TALUNOR_APPROVAL=highrisk go run ./cmd/talunor --plain
```

```text
you> what is 12 * 8? use the calculator.
📋 Plan:
goal: what is 12 * 8? use the calculator.  (confidence 1.00)
  1. [tool] calculator({"expression": "12 * 8"}) — compute the product
  2. [final] — report the result
🔧 calculator({"expression":"12 * 8"})
   ↳ 96
The result of 12 multiplied by 8 is 96.
you> /plan
```

`/plan` réaffiche le dernier plan. Sens maintenant l'**approbation du plan entier** :
relance avec le mode par défaut et donne-lui une tâche qui utilise un outil :

```bash
TALUNOR_PLANNER=1 go run ./cmd/talunor --plain   # TALUNOR_APPROVAL vaut plan par défaut
```

Cette fois, avant que quoi que ce soit ne tourne, on te demande d'approuver le plan
entier — réponds `n` et regarde-le refuser d'avancer. Enfin, vois la **policy**
bloquer un plan avant exécution. Écris un fichier de règles qui refuse la
calculatrice (Leçon 12) :

```bash
printf 'rules:\n  - tool: calculator\n    action: deny\n    reason: no math today\n' > deny.yaml
TALUNOR_PLANNER=1 TALUNOR_POLICY=./deny.yaml go run ./cmd/talunor --plain
```

Demande un calcul : le plan est produit, le pré-filtrage voit l'étape refusée, et le
tour se termine par une explication — l'outil n'a jamais tourné, et on ne t'a même
pas demandé d'approuver. Deny l'emporte sur plan.

## Les principes

```text
L'exécution émergente révèle son plan en agissant ; l'exécution délibérée l'énonce d'abord.
```

1. **L'approbation au niveau plan n'est sûre que grâce au cap qui garde l'exécution
   dans le plan.** Impose la frontière à la surface de l'API (les outils offerts), pas
   en faisant confiance au modèle pour rester sur le script.
2. **Une sortie structurée fiable est une discipline, pas un prompt.** Contrat strict
   → extraction tolérante → vraie validation → un retry qui renvoie l'erreur.
3. **Réutilise la boucle en laquelle tu as confiance.** Le planner n'a pas remplacé
   ReAct ; l'exécution réutilise le même cœur, en ne changeant que les outils offerts
   et la façon de demander l'approbation.
4. **Conçois l'échec comme une dégradation.** Un mauvais plan retombe sur ReAct
   classique ; l'utilisateur a quand même une réponse.
5. **Délibéré et émergent sont tous deux valides — livre l'interrupteur.**
   `TALUNOR_PLANNER` te laisse lancer le même prompt des deux façons et choisir selon
   la situation.

## Checklist de fin

- [ ] Je sais opposer l'exécution émergente (ReAct) et délibérée (plan d'abord) et
      nommer le compromis.
- [ ] J'ai lu `planner.go` et je sais lister les quatre temps de l'obtention d'un plan
      valide depuis un LLM.
- [ ] Je sais expliquer pourquoi `decodePlan` utilise un `json.Decoder` plutôt qu'une
      regex.
- [ ] J'ai lu `runPlanned` et je sais énoncer ses quatre phases.
- [ ] Je sais expliquer le cap structurel et pourquoi l'approbation-plan en dépend.
- [ ] J'ai lancé les tests du planner, et lancé l'agent en live avec un plan (et vu
      `/plan`).
- [ ] J'ai vu un `deny` de la policy bloquer un plan avant exécution.
- [ ] Je suis revenu à `main`.

---

## 🎓 À propos de cette leçon

Ceci clôt l'**Itération 3** : l'agent a désormais un garde-fou (la policy) et de la
préméditation (le planner). Remarque ce que le plan t'a donné que ReAct ne pouvait
pas — un artefact unique à *inspecter, approuver, caper, et refuser*. C'est la forme
récurrente de l'autonomie sûre : rendre l'intention explicite, puis contraindre
l'exécution à celle-ci.

Les limites honnêtes valent d'être retenues : le cap de Talunor est **structurel**
(seuls les outils du plan sont offerts), pas **sémantique** (il ne juge pas si un
appel prévu s'est éloigné de l'intention), et il ne re-planifie pas quand une étape
surprend. Ces points — plus te laisser éditer un plan à la main avant qu'il ne tourne
— sont différés à des incréments ultérieurs, et chacun est une belle leçon en
attente. Ensuite vient l'Itération 4 : l'apprentissage — consolider la mémoire, et
apprendre des plans que l'agent a exécutés.

Retour à l'[index du cours](../).
