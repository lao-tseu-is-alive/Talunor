# Leçon 21 — La parole de qui compte ? Un modèle de confiance est une décision, pas un défaut

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration + pratique** (lecture de `internal/memory`, `internal/agent` ; le code de
la Couche 21 est livré à `v0.20.0`, docs de référence sur `main`) · Niveau 3 (avancé) ·
~65 min

## Pourquoi cette leçon existe

La Couche 20 a appris à l'agent à *apprendre* de ce qu'il observe. La Couche 21 lui donne un
pouvoir plus dangereux : se **corriger** — un nouveau fait peut *retirer* un ancien qui le
contredit. Dès l'instant où la mémoire peut retirer une croyance, une question décide si
elle reste digne de confiance ou pourrit en silence :

> **Qui a le droit de corriger qui ?**

La réponse tentante est un classement global unique — « l'utilisateur passe avant le modèle,
l'outil avant rien », choisis un ordre et applique-le partout. Cette leçon explique pourquoi
cette réponse est un *piège*, pourquoi elle casse dans **deux directions opposées**, et
pourquoi le remède n'est pas un classement plus malin mais une politique petite, explicite
et *cadrée* que tu décides exprès. C'est une méta-leçon, dans l'esprit de la Leçon 15 : ce
qu'il faut emporter est une **habitude de pensée**, pas un mécanisme.

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer pourquoi un classement de provenance global unique échoue dans les *deux* sens
  (deux exemples concrets) ;
- énoncer le principe qui résout les deux — **l'autorité est par-domaine ; la provenance en
  est un proxy** — et où vit le modèle de confiance dans le code (une fonction) ;
- lire `memory.Supersedes` et expliquer comment l'arbitre *propose* pendant que le modèle de
  confiance *autorise* ;
- expliquer comment une fausse affirmation de l'utilisateur sur le monde est mémorisée
  fidèlement sans corrompre un fait du monde ;
- passer une **checklist** sur n'importe quelle conception de mémoire d'agent *avant* de la
  construire — et prouver, en modifiant ~10 lignes, que le modèle de confiance est porteur.

## Prérequis

- **Leçon 16** (provenance & confiance) — la provenance est assignée par le système, à partir de la source.
- **Leçon 17** (apprendre avec humilité) — la règle d'indépendance ; ici elle passe de la
  *confiance* à la *vérité*.
- **Leçon 20** (apprendre de l'action) — là où les faits du monde/d'observation entrent en
  mémoire, ce qui fait mordre cette question.

## Partie 1 — la conception qui marche (jusqu'à ce que non)

Pour un assistant personnel, un classement global semble évidemment juste : **l'utilisateur
passe avant le modèle.** Si le modèle a supposé « User is a Go beginner » et que l'utilisateur
dit « je suis expert », l'utilisateur gagne. Correct. Lis les niveaux sur `main` :

```text
internal/memory/memory.go   (Provenance, BaseConfidence)
```

`user_stated` au-dessus de `model_inferred` — l'utilisateur est l'autorité. Un temps, c'est
tout ce que la mémoire contient (des faits *sur l'utilisateur*), et le classement n'est
jamais faux. Puis la Couche 20 a laissé entrer les faits du monde et d'observation, et le
classement rencontre la réalité.

## Partie 2 — ça casse dans les DEUX sens

Deux exemples. Un classement unique ne peut satisfaire les deux — c'est tout l'enjeu.

**La terre plate.** L'utilisateur dit « la terre est plate ». Si `user_stated` passe
globalement avant tout, ça **écrase un fait du monde correct**. Mais l'utilisateur est
autorité sur *lui-même*, pas sur le monde. Un « l'utilisateur gagne » global *corrompt* la
mémoire.

**La signature d'attaque.** Un outil de détection d'intrusion vérifié observe « la signature
X est mitigée par le comportement Y ». C'est une preuve réelle, de haute autorité, qui
**devrait** pouvoir retirer une croyance périmée inférée par le modèle sur cette signature.
Si tu réagis au cas de la terre plate en décrétant « la mémoire est un modèle de
l'utilisateur, ne jamais stocker de faits du monde », tu perds exactement ça — et un vrai
agent doit se souvenir de ce qu'il a observé.

Un exemple te pousse à *baisser* l'autorité de l'utilisateur ; l'autre à *garder* haute
l'autorité d'une source. Aucun ordre global unique ne fait les deux. L'échec n'est pas
l'ordre choisi — c'est d'avoir choisi **un ordre pour tous les domaines**.

## Partie 3 — le principe : l'autorité est par-domaine

Ce que les deux cas partagent : **l'autorité dépend du domaine de l'affirmation, et la
provenance d'un fait est un *proxy* de son autorité *dans ce domaine*.**

- L'utilisateur est autorité sur **l'utilisateur**, pas le monde.
- Un outil vérifié est autorité sur **ce qu'il a observé**, pas les préférences de l'utilisateur.
- Le modèle est autorité sur **très peu** tout seul — ses inférences sont des hypothèses.

Donc le modèle de confiance n'est pas un classement global ; c'est une **politique** qui dit
qui compte *pour quoi*. Et le geste d'ingénierie honnête est de mettre cette politique dans
**un seul endroit petit, nommé et documenté** — pour que ce soit une décision qu'on peut
lire, tester et posséder, pas une hypothèse étalée dans le code.

## Partie 4 — lis le modèle de confiance (c'est une fonction)

```text
internal/memory/supersede.go   (Supersedes, supersedeAuthority)
internal/agent/arbiter.go      (FactArbiter, le verdict SUPERSEDES)
internal/agent/agent.go        (learnOneFact — proposer, puis gater)
docs/decisions/0003-trust-model-for-supersession.md
```

Deux choses coopèrent, et la séparation *est* la sécurité :

1. **L'arbitre *propose*.** Un pas LLM classe un nouveau fait face à un voisin proche :
   `RESTATES` / `SUPERSEDES` / `UNRELATED`. `SUPERSEDES` exige que les deux soient sur le
   **même sujet** et **incompatibles**.
2. **`memory.Supersedes` *gate*.** Il ne répond qu'à la question d'autorité — une source de
   provenance *newer* peut-elle retirer un fait de provenance *older* ? Par défaut :

   ```
   user_stated / tool_observed  → autoritaire (peut retirer ≤ son niveau)
   unspecified                  → ne peut retirer que le modèle
   model_inferred               → ne retire RIEN (la règle d'humilité)
   ```

Le modèle *propose* ; le système *décide de ce qui est autorisé*. Une inférence du modèle qui
contredit un fait utilisateur est **abandonnée** — pas stockée en rivale — parce que le
modèle n'a pas à écraser l'utilisateur sur la foi de sa propre supposition.

Et la terre plate ? Elle n'atteint jamais le gate. L'affirmation de l'utilisateur sur le
monde est stockée comme une **croyance sur l'utilisateur** (« User believes the earth is
flat »), qui est un *sujet différent* d'un fait du monde → l'arbitre répond `UNRELATED`, et
ils **coexistent**. L'agent se souvient de ta vision du monde sans l'adopter comme *le*
monde. La signature d'attaque, elle, atteint le gate, en tant que `tool_observed` d'un outil
vérifié, et est autorisée à retirer la croyance périmée. Même machinerie, résultats opposés —
le signe que la couture est au bon endroit.

**Regarde le peu de lignes que c'est.** `Supersedes` + `supersedeAuthority` est tout le
modèle de confiance. Un agent de sécurité ou d'ops remplace *cette* fonction — et rien d'autre.

## Partie 5 — la checklist à emporter (le vrai acquis)

Avant de bâtir une mémoire dans un agent, réponds à ceci. Écris les réponses — cet acte
*est* le point.

1. **Quels types de faits cette mémoire tiendra** — soi, monde, observations de domaine ?
2. Pour chaque type, **qui est l'autorité** — et est-ce la *même* source pour tous les types ?
3. Mon mapping provenance→confiance est-il une **politique consciente**, ou ai-je hérité de
   « l'utilisateur a toujours raison » ?
4. Quand la source A contredit la source B, **qui gagne — et cette réponse dépend-elle du domaine** ?
5. Une **source de basse autorité peut-elle corrompre en silence un fait de haute autorité** ?
   *(l'échec terre plate)*
6. Une **observation réellement autoritaire peut-elle mettre à jour une croyance périmée** ?
   *(l'échec signature d'attaque)*
7. **Où, dans mon code, vit le modèle de confiance** — un seul endroit explicite, ou éparpillé
   et implicite ?

Si la réponse au #7 est « éparpillé et implicite », tu *as* un modèle de confiance — tu ne
l'as juste pas écrit. Écris-le, et cadre-le.

## Partie 6 — prouve qu'il est porteur

D'abord, les tests encodent la politique :

```bash
go test ./internal/memory/ -run 'Supersede' -v
go test ./internal/agent/  -run 'Supersede|Unrelated' -v
```

Lis `TestSupersedeGateProtectsUser` : un fait `model_inferred` qui contredit un `user_stated`
est **abandonné**, et le fait de l'utilisateur reste actif. Lis `TestUnrelatedStoresNew` : un
fait proche-mais-non-lié est gardé comme sa propre ligne (la coexistence terre plate,
généralisée). `TestSupersedesTrustModel` est toute la politique en un tableau.

Maintenant fais *échouer le modèle de confiance exprès* — c'est l'exercice qui fixe. Dans
`internal/memory/supersede.go`, modifie `supersedeAuthority` pour que
`ProvenanceModelInferred` renvoie `2` (aussi autoritaire que l'utilisateur), puis relance :

```bash
go test ./internal/agent/ -run 'SupersedeGateProtectsUser' -v
```

Ça **ÉCHOUE** maintenant : l'inférence du modèle est autorisée à écraser ce que l'utilisateur
a dit. Tu as vu « la parole de qui compte » basculer — *en dix caractères*. Reviens en
arrière. Cette seule édition est la leçon : le modèle de confiance n'est pas de la décoration ;
c'est l'épistémique de ton agent, et c'est à toi de la décider.

En direct (nécessite Ollama) : dis un fait à l'agent, puis contredis-le, et inspecte :

```text
you> mon langage préféré est Python
you> en fait mon langage préféré est Go maintenant
you> /list       # le fait Python est marqué ⚠→#N (superseded)
you> /why <id du fait Python>   # montre ce qui l'a retiré
```

## Les principes

```text
Une mémoire qui se corrige a besoin d'un modèle de confiance — et un classement global « utilisateur > modèle » en est un, caché et cassé.
```

1. **Un classement de provenance global unique est un modèle de confiance caché, et il casse
   dans les deux sens** — il corrompt les faits du monde (terre plate) ou interdit de vraies
   observations (signature d'attaque).
2. **L'autorité est par-domaine ; la provenance en est un proxy.** Qui compte dépend de *ce
   sur quoi* porte l'affirmation.
3. **Mets le modèle de confiance dans un seul endroit nommé et testable.** « La parole de qui
   compte » est une décision que tu possèdes, pas une hypothèse étalée dans le code.
4. **Quand le LLM gagne un pouvoir destructeur, gate-le avec une règle non-LLM et rends-le
   réversible.** L'arbitre propose ; le modèle de confiance gate ; la supersession est réversible.

## Checklist de complétion

- [ ] Je sais donner deux exemples où un classement de provenance global unique échoue, en sens opposés.
- [ ] Je sais énoncer le principe résolvant (l'autorité est par-domaine ; la provenance en est un proxy).
- [ ] J'ai lu `memory.Supersedes` et je sais expliquer le partage proposer-puis-gater avec l'arbitre.
- [ ] Je sais expliquer comment une fausse affirmation de l'utilisateur sur le monde est mémorisée sans corrompre un fait du monde.
- [ ] J'ai passé la checklist sur une conception de mémoire (celle de Talunor, ou la mienne).
- [ ] J'ai modifié `supersedeAuthority` et vu `TestSupersedeGateProtectsUser` échouer — puis j'ai reverté.

---

## 🎓 À propos de cette leçon

C'est une méta-leçon, et son vrai sujet est une erreur que tu ferais autrement *par défaut*.
Presque chaque tutoriel « donne une mémoire à l'agent » attrape le classement évident —
l'utilisateur est la source de vérité — parce que pour un assistant personnel jouet ce n'est
jamais faux. Ça devient faux à l'instant où la mémoire tient quoi que ce soit dont
l'utilisateur n'est pas l'autorité, c'est-à-dire l'instant où l'agent devient intéressant. Le
bug ne s'annonce pas ; il surgit plus tard sous forme d'une mémoire pleine avec assurance des
idées fausses de l'utilisateur, ou d'un agent incapable de retenir ce que ses propres outils
lui ont dit.

L'habitude que cette leçon veut te laisser est petite et durable : **avant de laisser une
mémoire se corriger, décide — explicitement, et par-domaine — la parole de qui compte, et
mets cette décision dans un seul endroit que tu peux lire.** La réponse de Talunor est dix
lignes dans `supersede.go` ; la tienne sera différente, parce que ton agent est pour autre
chose. La leçon n'est pas les dix lignes. C'est que tu les as *écrites exprès*.

Ensuite, l'Itération 5 passe de *à qui faire confiance* à *ce que tu peux trouver* : la
**Leçon 22** (prévue) ajoute le rappel hybride — vectoriel *et* lexical — pour que
l'identifiant exact, le nom rare, le nombre que tu raterais par le sens seul restent
récupérables.

Retour à l'[index du cours](../README.fr.md).
