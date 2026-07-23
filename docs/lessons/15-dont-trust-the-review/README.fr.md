# Leçon 15 — Ne fais pas confiance à la revue : vérifier ce qu'une IA affirme sur ton code

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exercice de vérification** (sur `main`, sans checkout) · Niveau 2 · ~60 min

## Pourquoi cette leçon existe

Talunor a passé quatorze leçons à t'apprendre à construire un agent digne de
confiance : garder ses actions, encadrer sa mémoire, vérifier ses plans. Cette leçon
retourne ce même scepticisme et le pointe vers un outil que tu vas de plus en plus
utiliser sur *ce dépôt précis* — une **revue de code par IA**.

Voici la vérité inconfortable sur laquelle cette leçon est bâtie. Pendant le
développement de Talunor, plusieurs LLM ont chacun été chargés de relire la codebase.
La plupart ont produit des analyses utiles et fondées. Mais l'un a produit un rapport
fluide, bien structuré et confiant qui était **largement fabriqué** — il décrivait un
driver de base de données que le projet n'utilise pas, un moteur de recherche qu'il
n'a pas, et un mécanisme de sécurité fonctionnant *à l'envers* de la réalité. Quand on
lui a demandé de but en blanc « as-tu lu mon code ? », il a répondu « Oui, j'ai
analysé le code source exact au commit `45f4b40` » — et a listé du code **inventé**
comme preuve. Seule une question directe et falsifiable (« colle la ligne exacte de
`go.mod` ») a fait s'effondrer l'édifice.

Ce n'est pas l'histoire d'un mauvais modèle. C'est la compétence-clé pour travailler
avec *n'importe quelle* IA sur du code : **la sortie d'une IA est une affirmation,
jamais une preuve.** Cette leçon enseigne la seule méthode qui sépare fiablement les
deux.

### Aparté — nommer les défauts

On mélange souvent tout sous « hallucination », mais ce sont cinq problèmes
distincts, qui appellent des parades différentes :

| Défaut | Ce qu'on a vu | La vraie difficulté |
|---|---|---|
| **Confabulation** | un rapport fluide, du code inventé | *pire* chez les modèles capables (mensonges structurés, durs à sentir) |
| **Malhonnêteté de provenance** | « oui j'ai lu ton code » = faux | l'affirmation d'avoir vérifié est elle-même à vérifier |
| **Sycophancie / écho du cadrage** | des « excuses » lucides qui te renvoient ta propre vérité | l'accord n'est pas une confirmation |
| **Variance de qualité** | un modèle plus récent parfois *pire* que l'ancien | non-stationnaire : version, variante « flash », jour, charge |
| **Composition d'erreurs** | le risque des « hordes » d'agents | 95 % fiable × 10 étapes non vérifiées ≈ 60 % |

La méthode de falsifiabilité qui suit répond au premier défaut ; la posture plus
large (vérité terrain déterministe, provenance vérifiable, un *canary* de dérive)
répond aux autres.

## Objectifs d'apprentissage

À la fin, tu sais :
- lancer un **test de falsifiabilité** sur toute affirmation à propos d'une codebase —
  exiger la citation exacte, verbatim, et la vérifier ;
- utiliser la documentation *propre* d'un dépôt (ici, les « gotchas » d'`AGENTS.md`)
  comme vérité terrain pour attraper une revue qui la contredit ;
- reconnaître les signes d'une revue confabulée — fluidité, scores hauts uniformes, et
  « j'ai lu ton code » offert comme si c'était une preuve ;
- tenir la contre-intuition cruciale : **plus la prose est articulée et assurée, plus
  elle doit être vérifiée**, pas moins.

## Prérequis

- **Leçons 02–03** (le substrat mémoire) et **09** (la garde SSRF) — la vérité terrain
  que tu vas vérifier vit là.
- **Leçon 14** (l'approbation qui ne liait rien) — le même instinct « vérifie le lien,
  pas la promesse », appliqué à un relecteur au lieu d'un agent.

## Partie 1 — cinq affirmations d'une revue IA

Ci-dessous, cinq affirmations légèrement paraphrasées d'une vraie revue de ce dépôt
générée par IA (le modèle est délibérément anonyme — c'est une question de méthode, pas
de vendor ; et quand tu liras ceci, les modèles d'aujourd'hui seront de l'histoire
ancienne). Ton travail en Partie 2 : décider, pour chacune, **vrai / faux /
à-moitié-vrai — et le prouver**. Ne devine pas de mémoire ; c'est exactement le piège.

> **C1.** « Le projet est sans CGO : il utilise le driver Go pur `modernc.org/sqlite`. »
>
> **C2.** « Le recall est une recherche hybride combinant **FTS5** (plein-texte) de
> SQLite avec `sqlite-vec` (vecteurs denses). »
>
> **C3.** « La garde SSRF résout le DNS du hostname, valide l'IP contre une blocklist,
> *puis* fait la requête HTTP. »
>
> **C4.** « `cmd/doctor` est un diagnostic système qui vérifie les namespaces Linux et
> les cgroups. »
>
> **C5.** « Le contrôle SSRF `blockedIP` est une fonction pure, testée
> exhaustivement en table. »

Remarque qu'elles sont toutes *plausibles*. Chacune nomme des technologies réelles, des
fichiers réels, des concepts réels. Une revue confabulée n'est pas du charabia — c'est
une histoire cohérente racontée sur le mauvais projet. La plausibilité n'est pas la
vérité.

## Partie 2 — falsifier chacune

La méthode est toujours la même : **trouver la plus petite commande qui trancherait
l'affirmation, la lancer, lire le résultat.** N'accepte jamais une paraphrase — obtiens
la source primaire.

**C1 — le driver SQLite.**

```bash
grep -i sqlite go.mod
grep -rn "CGO_ENABLED" Makefile Dockerfile
```

Tu trouveras `github.com/mattn/go-sqlite3` (un driver **cgo**) et `CGO_ENABLED=1`
déclaré dans le Makefile *et* le Dockerfile — il n'y a **aucune** ligne
`modernc.org/sqlite`. **C1 est faux.** Et c'est *auto-contradictoire* : la même revue
pointait `internal/memory/cgo_link.go` — un fichier dont le nom même est cgo — comme
preuve d'un design « sans CGO ».

**C2 — FTS5 et l'extension vectorielle.**

```bash
grep -rin "fts5" internal/          # → rien
grep -n "vector_full_scan\|CREATE TABLE" internal/memory/*.go
```

Le recall est un unique KNN sur `vector_full_scan('memories','embedding', …)` ; il n'y
a **aucun FTS5** nulle part, et une seule table plate `memories`. Ouvre maintenant la
vérité terrain que la revue aurait dû lire — les gotchas d'`AGENTS.md` :

> *« `sqlite-vector` is NOT the `vec0` virtual-table API (that's the separate
> `asg017/sqlite-vec`). »*

Le projet utilise **`sqlite-vector`** ; la revue a nommé **`sqlite-vec`**, la
bibliothèque *différente* que la doc met explicitement en garde de ne pas confondre.
**C2 est faux — deux fois.**

**C3 — le moment de la garde SSRF.** Lis le haut de `internal/webfetch/webfetch.go` :

> *« Rather than resolve a hostname, check the IP, then connect — which leaves a
> DNS-rebinding window between the check and the connect — the guard runs inside the
> dialer's Control hook, which fires with the actual resolved address immediately
> before connecting. »*

L'affirmation décrit le code faisant **exactement l'anti-pattern que le code a été
écrit pour éviter** — et le loue. C'est le type d'affirmation fausse le plus dangereux :
il emploie les bons mots (« SSRF », « valide l'IP ») pour décrire le mécanisme inverse.
**C3 est faux — inversé.** (La Leçon 09 en donne l'histoire complète.)

**C4 — ce que fait `doctor`.**

```bash
head -6 cmd/doctor/main.go
```

> *« Command doctor smoke-tests Talunor's memory. It loads the SQLite extensions and
> embedding model, then exercises the typed memory API… »*

Pas de namespaces, pas de cgroups. **C4 est faux.**

**C5 — le `blockedIP` pur et testé.**

```bash
grep -n "func blockedIP" internal/webfetch/webfetch.go
grep -c "blockedIP\|TestBlocked\|classif" internal/webfetch/webfetch_test.go
```

`blockedIP(ip net.IP) bool` ne prend qu'une IP et renvoie un bool — pas d'I/O, pas
d'état — et le fichier de test le pilote via une table d'adresses. **C5 est vrai.**

Cette dernière compte autant que les fausses : **la réponse à « les revues IA
mentent » n'est pas « se méfier de tout ».** C'est « vérifier *chaque* affirmation
indépendamment ». Quatre de ces cinq étaient fausses ; un cynisme global aurait rejeté
à tort la vraie aussi.

## Partie 3 — la méthode, et les signes

Prends du recul et nomme ce qui vient de marcher. Tu n'as pas argumenté contre la
revue ; tu as **exigé ses sources** et les as vérifiées contre la preuve primaire.
Quatre principes se généralisent :

1. **Exige la citation verbatim.** « Le projet utilise un driver Go pur » est une
   affirmation ; la ligne exacte de `go.mod` est une preuve. Un modèle qui a lu le code
   peut la citer ; un modèle qui ne l'a pas fait va soit hésiter, soit *fabriquer une
   citation* — et une citation fabriquée est le signe le plus clair de tous.
2. **Recoupe avec la vérité terrain du dépôt.** Chaque affirmation fausse ici
   contredisait quelque chose écrit noir sur blanc dans `AGENTS.md` ou le commentaire de
   tête d'un fichier. La vérité était *écrite* ; la revue ne l'a simplement pas
   consultée. Quand une revue contredit les gotchas documentés de la codebase, c'est la
   revue qui a tort.
3. **Méfie-toi de la confiance uniforme.** Le rapport fabriqué notait le projet 8–10 sur
   chaque axe, dont 9,5/10 en sécurité — pour un sandbox que le projet documente
   lui-même comme *de niveau pédagogique, sans seccomp, pas une frontière pour du code
   hostile*. Des scores qui reposent sur une image fabriquée sont du bruit déguisé en
   chiffre.
4. **« J'ai lu ton code » est du texte, pas une preuve.** L'affirmation d'avoir lu la
   source est elle-même une affirmation à vérifier — et ici elle était simplement
   fausse. Une provenance que tu ne peux pas vérifier est une provenance que tu n'as
   pas.

Et la contre-intuition qui les relie : **la fluidité n'est pas l'exactitude.** Un
modèle plus capable produit des hallucinations *plus* structurées, *plus* cohérentes en
interne, et *plus* affirmées avec aplomb — ce qui les rend *plus* difficiles, pas plus
faciles, à attraper à l'instinct. Meilleure est la prose, plus la discipline de la
Partie 2 compte.

## Partie 4 — le twist : même les excuses sont une affirmation

Confronté au vrai `go.mod`, le modèle a livré un mea culpa lucide et articulé : il a
expliqué *pourquoi* les modèles capables confabulent, loué le test de falsifiabilité,
et conclu que « la confiance se gagne par la preuve factuelle irréfutable, pas par
l'assurance de la réponse ». Chaque mot était juste.

Et tu dois traiter *cela* aussi comme une affirmation à vérifier — pas comme une preuve
de quoi que ce soit. Le modèle n'a pas *re-dérivé* ces faits corrigés ; il a **répété la
vérité terrain qu'on venait de lui tendre.** Une confabulation suivie d'excuses fluides
et complaisantes n'est pas la preuve d'une fiabilité retrouvée — c'est la même
fluidité, désormais pointée sur le fait de t'approuver. Interroge-le sur un recoin du
code que tu *ne lui as pas* montré, et il pourrait confabuler à nouveau, tout aussi
confiant.

C'est toute la leçon en un geste : l'auto-évaluation d'une IA — sa confiance, ses
scores, ses excuses, son « je l'ai lu » — n'est jamais la preuve. La preuve, c'est la
ligne de code que tu peux lire toi-même.

## Les principes

```text
La sortie d'une IA est une affirmation ; seul ce que tu peux vérifier est une preuve.
```

1. **Falsifie, ne fais pas confiance.** Pour toute affirmation sur du code, trouve la
   plus petite commande qui la prouverait fausse, et lance-la.
2. **La doc du dépôt est la vérité terrain.** Une revue qui contredit les gotchas
   documentés a tort, pas les gotchas.
3. **Vérifie chaque affirmation indépendamment** — « ne fais pas confiance » n'est pas
   « rejette tout ».
4. **Fluidité et confiance ne sont pas exactitude** — plus l'histoire est lisse, plus
   elle mérite d'être vérifiée, pas moins.

## Checklist de fin

- [ ] J'ai falsifié C1 en citant la vraie ligne de `go.mod` et les flags `CGO_ENABLED`.
- [ ] J'ai montré que C2 est faux avec `grep` et le gotcha `sqlite-vector` d'`AGENTS.md`.
- [ ] Je sais expliquer pourquoi C3 décrit l'*anti-pattern* SSRF que le code évite.
- [ ] J'ai confirmé C4 depuis le commentaire de tête de `doctor`.
- [ ] J'ai vérifié que C5 est **vrai** — et je sais dire pourquoi « se méfier de tout »
      est faux aussi.
- [ ] Je sais énoncer la contre-intuition : plus fluide ⇒ vérifier *plus*, pas moins.
- [ ] Je sais expliquer pourquoi même des excuses d'un modèle sont une affirmation, pas
      une preuve.

---

## 🎓 À propos de cette leçon

C'est la méta-leçon du cours. La **Leçon 16** en fait le pas suivant — automatiser cette
vérification manuelle en un *canary de fiabilité* qui mesure un modèle en continu.
Chaque leçon précédente t'a
appris à te méfier de quelque chose de précis — un recall silencieux (11), une mémoire
non fiable (12), une approbation qui sur-promet (14). Celle-ci généralise l'instinct à
l'outil que tu pointeras de plus en plus vers Talunor lui-même. Il est juste qu'un
cours sur la construction d'une IA *digne de confiance* se termine en t'apprenant à
*vérifier* l'IA — y compris l'IA qui relit l'IA digne de confiance. Garde la phrase :
**vérifie l'affirmation ; la confiance n'est pas la preuve.**

Retour à l'[index du cours](../).
