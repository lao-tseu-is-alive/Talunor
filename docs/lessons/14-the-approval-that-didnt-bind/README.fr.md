# Leçon 14 — L'approbation qui ne liait rien : post-mortem sécurité du mode plan

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** (le bug à `v0.13.1`, le correctif sur `main` /
`v0.13.2`) · Niveau 3 (avancé) · ~60 min

## Pourquoi cette leçon existe

La Leçon 13 a livré le planner et t'a dit, fièrement, que son approbation du plan
entier permet à « l'humain de voir le plan complet — les outils et arguments exacts —
et de l'approuver une fois ». Cette phrase n'était **pas tout à fait vraie**, et
l'écart était de sécurité : dans le mode par défaut `plan`, approuver le plan liait
les *outils* que l'agent pouvait utiliser, mais **pas les arguments** avec lesquels
il allait réellement les exécuter.

Cette leçon est le post-mortem de cet écart — comment il s'est glissé, pourquoi il
contredit la Leçon 12 du projet lui-même, comment une revue croisée multi-modèles
l'a débusqué, et comment un petit changement l'a refermé. Comme la Leçon 11, elle est
tirée d'un vrai défaut de l'histoire de Talunor, et le plus précieux ici n'est pas le
correctif — c'est l'*instinct* qu'elle enseigne : **une approbation ne protège que ce
qu'elle lie mécaniquement.**

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer la différence entre lier le **nom** d'un outil et lier ses **arguments**,
  et pourquoi le second est ce qu'une approbation signifie d'ordinaire ;
- décrire la forme *confused deputy* du bug : une décision humaine sur une chose (le
  plan affiché) qui pilote un effet sur une autre (l'appel outil live) ;
- lire le mécanisme d'une ligne (`skipStepApproval`) qui a effondré une approbation à
  deux niveaux en une plus faible, et le seuil qui l'a restaurée ;
- expliquer pourquoi « c'est une limite documentée » n'est pas la même chose que « ce
  n'est pas un défaut » ;
- retenir pourquoi l'auteur d'un garde-fou est le plus mal placé pour en trouver le
  trou.

## Prérequis

- **Leçon 12 (la policy / l'open bar)** — le modèle de menace que ce bug viole.
- **Leçon 13 (le planner)** — le code où ce bug vit.

## Partie 1 — la promesse, et le mécanisme (lis le bug à `v0.13.1`)

```bash
git checkout v0.13.1        # detached HEAD — lecture seule (voir Leçon 00)
```

Rappelle-toi la forme d'un tour planifié (`internal/agent/execute.go`, `runPlanned`)
: plan → pré-filtrage policy → **approbation du plan entier** → exécution ReAct capée.
En mode `plan`, l'exécution était configurée ainsi :

```go
exec := execCtx{skipStepApproval: a.cfg.ApprovalMode == ApprovalPlan}
```

et chaque appel outil passait par `runTool` (`internal/agent/agent.go`) :

```go
if d.NeedsApproval() && !exec.skipStepApproval {
    req := llm.NewApprovalRequest(name, string(args))   // args = les args LIVE du modèle
    ...
}
return a.tools.Execute(ctx, name, args)                  // exécute les args LIVE
```

Lis ces deux fragments ensemble et l'écart apparaît :

1. En mode `plan`, `skipStepApproval` vaut `true`, donc `runTool` **saute
   entièrement le prompt par-étape** — pour *tous* les niveaux de risque, y compris le
   shell (`bash`).
2. Le plan approuvé par l'humain montrait les arguments proposés par le *planner*.
   Mais l'exécution est une **boucle ReAct** : le modèle choisit les arguments *live*,
   et `a.tools.Execute` exécute *ceux-là* — jamais re-vérifiés contre ce qui a été
   approuvé.

Donc le cap d'outils (`allowTools`, par nom) tenait — le modèle ne pouvait pas
appeler un outil hors-plan. Mais rien ne tenait les *arguments*. Un plan affichant
`bash({"cmd":"ls"})` pouvait exécuter `bash({"cmd":"rm -rf /"})`, et l'humain — qui a
approuvé « le plan » — n'a jamais vu la seconde commande.

> **L'idée centrale.** L'approbation du plan entier liait le **nom** de l'outil, pas
> ses **arguments**. L'humain a consenti à une *représentation* (le plan affiché) ; le
> système a produit un *effet* (l'appel live). La distance entre les deux était la
> vulnérabilité.

## Partie 2 — pourquoi c'est le principe du projet lui-même, violé

Ce n'est pas n'importe quel bug. La Leçon 12 a bâti toute la couche policy sur une
phrase : *ne jamais exécuter un outil sur la seule base du jugement du modèle.* Un
texte injecté (un souvenir rappelé, une page récupérée) peut convaincre un modèle
d'agir — c'est toute la raison d'être du garde-fou.

Regarde maintenant ce que faisait le mode `plan` : après un seul « oui » en amont, il
exécutait ce que le modèle décidait, arguments compris, sans autre contrôle. C'est
*exactement* ce que la Leçon 12 interdit — réintroduit par le garde-fou censé rendre
les choses plus sûres. Un modèle injecté pouvait proposer un plan anodin pour gagner
le « oui » de l'humain, puis exécuter autre chose dans l'ensemble d'outils approuvé.

C'est le classique **confused deputy** : un composant de confiance (l'exécuteur) est
amené à mésuser de son autorité parce que celle-ci a été accordée contre le mauvais
objet (la forme du plan au niveau des noms, pas les arguments de l'appel). Quand tu
trouves un contrôle de sécurité qui contredit un principe que la même base de code
enseigne ailleurs, ce n'est pas un détail — c'est un vrai défaut.

Quand tu as fini de lire le bug, reviens :

```bash
git switch main
```

## Partie 3 — le correctif (lis sur `main`)

Le correctif (`v0.13.2`) est petit. Le booléen `skipStepApproval` — un on/off
grossier — devient un **seuil de risque**, `reapproveAtOrAbove`
(`internal/agent/agent.go`) :

```go
if d.NeedsApproval() && d.RiskLevel >= exec.reapproveAtOrAbove {
    // re-prompt, en montrant les arguments LIVE
}
```

et `runPlanned` fixe ce seuil par mode (`internal/agent/execute.go`) :

| mode | approbation plan entier | `reapproveAtOrAbove` | effet |
|------|-------------------------|----------------------|-------|
| `plan` | oui | `RiskHigh` | bas/moyen portés par le plan ; **haut risque (bash) re-confirme args live** |
| `step` | oui | `RiskLow` | chaque pas risqué re-confirme (ceinture et bretelles) |
| `highrisk` | non | `RiskLow` | plan advisory ; policy par-appel comme avant |

La ligne clé est le `RiskHigh` du mode `plan`. Une approbation du plan entier couvre
encore les pas à risque bas et moyen (une calculatrice, un `web_fetch` en lecture
seule), donc l'UX reste légère. Mais un pas à **risque élevé** — le shell, qui
choisit des arguments arbitraires — re-demande confirmation à l'exécution *avec les
arguments qu'il est réellement sur le point d'exécuter*. L'humain approuve désormais
le plan (l'intention) **et** confirme l'effet dangereux (la vraie commande). C'est
l'approbation à deux niveaux que la Leçon 13 décrivait — désormais réellement
appliquée.

Remarque que la valeur zéro fait ce qu'il faut : la boucle ReAct classique
(planner off) passe `execCtx{}`, donc `reapproveAtOrAbove` vaut `RiskLow` et *chaque*
appel signalé par la policy prompte — le comportement d'avant le planner, inchangé.

### Le test de régression qui le fige

Lis `TestPlannedPlanModeReapprovesHighRiskLiveArgs`
(`internal/agent/execute_test.go`). C'est le bug, figé :

```go
// Le plan propose une commande anodine ; le modèle en exécute une dangereuse.
prov := ... ToolCall{Name: "danger", Args: `{"cmd":"rm -rf /"}`} ...
pl  := ... PlanStep{Tool: "danger", Arguments: `{"cmd":"ls"}`, ...} ...
cfg.ApprovalMode = ApprovalPlan
...
if !strings.Contains(stepArgs, "rm -rf") {
    t.Errorf("high-risk re-prompt args = %q, want the LIVE 'rm -rf' args, not the plan's 'ls'", stepArgs)
}
```

Lance-le :

```bash
go test ./internal/agent/ -run 'PlanMode' -v
```

L'assertion est tout le propos : le re-prompt doit montrer `rm -rf` — l'argument que
l'exécuteur a *réellement* choisi — pas le `ls` affiché par le plan. Avant le
correctif, il n'y avait aucun re-prompt ; l'assertion ne pouvait même pas être écrite
honnêtement.

## Partie 4 — comment il a été trouvé, et pourquoi ça compte

Voici la partie inconfortable. Cet écart a été livré en `v0.13.0`. Il a été écrit par
le même auteur que la Leçon 12 — la leçon qui *t'enseigne à ne pas faire ça*.
Connaître le principe n'a pas empêché la violation, car quand tu construis un
garde-fou, tu le relis contre *ce que tu voulais qu'il fasse*, pas contre *ce qu'il
fait mécaniquement*.

Il a été débusqué par une **revue croisée multi-modèles** : plusieurs relecteurs LLM
indépendants ont été chargés d'analyser le dépôt. Deux d'entre eux, raisonnant
séparément, ont signalé la même chose — et, de façon révélatrice, ils *n'étaient pas
d'accord sur sa gravité*. L'un le qualifiait de défaut de sécurité (l'approbation lie
des noms, pas des args) ; l'autre de « by design, documenté » (le cap d'outils est
réellement documenté comme structurel). Les deux avaient à moitié raison, et le
désaccord était le signal : **quand l'UX d'un garde-fou implique une propriété que son
mécanisme n'applique pas, une note enfouie « c'est structurel » ne le rend pas
non-défaut.** La résolution honnête n'était pas de débattre quel relecteur avait
raison — c'était de faire correspondre le mécanisme à la promesse, et de le dire
clairement.

Les enseignements transférables :
1. **Tu es le plus mauvais relecteur de ton propre garde-fou.** Tu le testes contre
   ton intention ; un adversaire (ou un relecteur indépendant) le teste contre son
   comportement.
2. **Des perspectives indépendantes battent une seule, si minutieuse soit-elle.**
   Aucun relecteur n'a tout trouvé ; la valeur était dans le *recoupement* et le
   *désaccord*.
3. **« Limite documentée » est une odeur, pas une défense.** Si tu dois documenter
   que ton contrôle de sécurité est plus faible qu'il n'y paraît, envisage de corriger
   le contrôle.

## Les principes

```text
Une approbation ne protège que ce qu'elle lie mécaniquement — pas ce qu'elle affiche.
```

1. **Lie l'effet, pas la représentation.** Un « oui » humain doit garder les
   arguments réels qui vont s'exécuter, pas un plan qui les a seulement proposés.
2. **Un booléen est un instrument grossier pour une décision graduée.**
   `skipStepApproval` (tout-ou-rien) cachait l'écart ; un *seuil* de risque exprimait
   la vraie intention.
3. **Un contrôle qui contredit ton propre principe énoncé est un défaut**, si bien
   documenté soit-il — surtout dans une base de code qui enseigne ce principe.
4. **Relis à travers des perspectives.** La revue de l'auteur est nécessaire mais pas
   suffisante ; la revue adverse et indépendante trouve les trous que l'intention
   dissimule.

## Checklist de fin

- [ ] Je sais expliquer la différence entre lier le nom d'un outil et ses arguments.
- [ ] J'ai lu le code de `v0.13.1` et je peux pointer la ligne `skipStepApproval` qui
      a causé l'écart.
- [ ] Je sais décrire la forme confused-deputy (représentation vs effet).
- [ ] J'ai lu le correctif et je sais expliquer ce que fait
      `reapproveAtOrAbove = RiskHigh`.
- [ ] J'ai lancé les tests `PlanMode` et je comprends pourquoi le re-prompt montre les
      args live.
- [ ] Je sais argumenter pourquoi « limite documentée » n'excusait pas le défaut.
- [ ] Je suis revenu à `main`.

---

## 🎓 À propos de cette leçon

C'est le deuxième post-mortem du cours (après la Leçon 11), et le premier tiré d'un
bug dans un *garde-fou* — ce qui en fait l'échec le plus emblématique que Talunor
pouvait avoir. Elle referme aussi une petite boucle : la Leçon 12 t'a appris à ne pas
faire confiance au jugement du modèle pour une action ; cette leçon montre ce qui
arrive quand une fonctionnalité ultérieure fait discrètement exactement cela, et
comment le correctif restaure le principe. Si tu intériorises une seule phrase de tout
l'arc sécurité (Leçons 09, 10, 12, 14), que ce soit celle-ci : *ne fais pas confiance
à la promesse — vérifie le lien.*

Retour à l'[index du cours](../).
