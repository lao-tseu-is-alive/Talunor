# Leçon 19 — Hors du chemin critique : apprendre en arrière-plan

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration + pratique** (lire `internal/agent` ; le code du Layer 18 est sorti à
`v0.18.0`, docs de référence sur `main`) · Niveau 3 (avancé) · ~70 min

## Pourquoi cette leçon existe

Depuis la Leçon 05, chaque tour fait deux appels modèle : un pour te *répondre*, et un
second, plus discret, pour *apprendre* — l'étape de réflexion qui distille des faits
durables de ce que tu as dit. Et jusqu'ici ce second appel était **synchrone** : il
tournait en queue de la goroutine du tour et tenait le canal de réponse ouvert jusqu'à ce
qu'il finisse. La réponse avait déjà défilé à l'écran, mais le tour n'était pas *terminé*
— l'invite ne revenait pas — tant que l'extraction des faits n'était pas achevée. Tu
attendais les devoirs de l'agent.

Le Layer 18 corrige ça en déplaçant l'apprentissage **hors du chemin critique** : la
réponse défile, le tour se termine, et l'apprentissage rattrape sur un worker de fond. Ça
ressemble à un changement d'une ligne — `go reflect()` — et c'est *presque* ça. La partie
intéressante est tout ce que ce « presque » cache : une connexion de base partagée qui ne
doit pas être corrompue, un arrêt qui ne doit pas perdre l'apprentissage, et une vue debug
qui ne peut plus fonctionner comme avant. C'est la leçon de concurrence du cours, et elle
récompense la lecture du *raisonnement* plus que celle du diff.

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer ce que « sur le chemin critique » coûte, et ce que déplacer un travail hors de
  lui change et ne change pas ;
- énoncer l'insight porteur — **la connexion unique est déjà le verrou** — et pourquoi ça
  veut dire que le worker n'a besoin d'*aucun* mutex supplémentaire autour du store ;
- lire le worker + la file bornée et expliquer pourquoi un worker unique et sérialisé (et
  non une goroutine par tour) est la bonne forme ;
- expliquer le **contrat de drain à l'arrêt** et pourquoi `go reflect()` perdrait
  silencieusement l'apprentissage à la sortie ;
- expliquer pourquoi les notes `/debug` inline de la réflexion ont dû migrer vers le log.

## Prérequis

- **Leçon 05** (la boucle de l'agent) — où vivent les deux appels modèle.
- **Leçons 17 & 18** (apprentissage) — ce que la réflexion *fait* ; cette leçon change
  *quand* elle tourne.
- **Leçon 02** (mémoire persistante) — le piège `SetMaxOpenConns(1)` est le pivot de cette leçon.

## Partie 1 — le coût que tu payais

Lis la queue du tour sur `main` :

```text
internal/agent/agent.go   (reactLoop, vers la fin)
internal/agent/execute.go (finishAnswer)
```

Après que la réponse finale a défilé et que le tour assistant est stocké, la boucle
atteint son étape d'apprentissage. Avant le Layer 18, elle appelait directement
`reflect(ctx, out, input)`, dans la même goroutine, *avant* le `close(out)` différé. Comme
`reflect` fait un second appel LLM (extraction de faits) et plusieurs requêtes store, le
canal — et donc le sentiment de l'appelant que le tour est fini — restait ouvert tout du
long.

C'est la définition même de *sur le chemin critique* : un travail que l'utilisateur
attend, alors même que son résultat ne change pas la réponse qu'il a déjà. L'apprentissage
est exactement ce genre de travail — précieux, mais pas quelque chose derrière quoi
l'invite suivante devrait attendre.

## Partie 2 — l'insight : la connexion unique est déjà le verrou

Voici le raisonnement qui façonne tout le design, et il mérite qu'on ralentisse.

La crainte naïve à faire la réflexion en concurrence est : *deux goroutines vont toucher
le store en même temps et le corrompre.* Rappelle-toi de la Leçon 02 pourquoi le store
épingle une connexion unique :

```text
internal/memory/store.go   (cherche SetMaxOpenConns(1))
```

Les extensions SQLite gardent le modèle, le contexte d'embedding et `vector_init` dans un
état **par connexion**, donc le pool est plafonné à une connexion. Et voici la partie qui
transforme un problème en cadeau :

> **L'idée centrale.** `database/sql` remet cette connexion unique à **un seul appelant à
> la fois**. Une goroutine qui veut la connexion alors qu'une autre la détient *bloque*
> simplement jusqu'à ce qu'elle soit libre. Donc un écrivain de fond (la réflexion) et un
> lecteur au premier plan (le rappel du tour suivant) sont **sérialisés pour toi** — pas
> par un mutex que tu as écrit, mais par le pool de connexions que tu as déjà. La réflexion
> async n'a besoin d'**aucun verrou supplémentaire autour du store.**

C'est pourquoi le layer est sûr, et pourquoi `go test -race ./internal/agent/` reste
propre. Ça recadre aussi *pourquoi le worker existe*. Si la correction était la raison, un
mutex suffirait. Le worker gagne sa place pour trois autres raisons :

1. **Backpressure** — une borne sur la quantité d'apprentissage non fait qui peut s'empiler.
2. **Ordre** — les réflexions ont lieu dans l'ordre des tours.
3. **Un drain propre à l'arrêt** — Partie 4.

Nommer la *vraie* raison garde le design honnête : le worker est un outil d'ordonnancement
et de cycle de vie, pas un dispositif de sûreté.

## Partie 3 — le worker et la file bornée

Lis maintenant la mécanique :

```text
internal/agent/agent.go   (reflectJob, reflectWorker, enqueueReflect ; les champs worker sur Agent ; New)
```

La forme est petite et délibérée :

- `New` crée un channel **borné** `reflectCh` (capacité `reflectQueueCap`, 8) et démarre
  **un** goroutine `reflectWorker`.
- `reflectWorker` est une simple boucle `for job := range reflectCh` : elle réfléchit sur
  chaque job à tour de rôle, en série. Un seul worker, donc les écritures store de la
  réflexion ne se courent jamais l'une l'autre.
- Le tour n'appelle plus `reflect` directement ; il appelle `enqueueReflect(input)`, qui
  pose un job sur le channel et rend la main *immédiatement*. La réponse est déjà à
  l'écran ; le tour se termine maintenant.

Pourquoi une file *bornée* plutôt qu'illimitée, ou une goroutine par tour ? Un humain
converse bien plus lentement que la réflexion ne se termine, donc la file ne se remplit
presque jamais. Si elle le faisait un jour (imagine un script qui enchaîne les tours), un
channel borné fait que `enqueueReflect` **bloque brièvement** — backpressure — plutôt que
de créer des goroutines non bornées ou de jeter l'apprentissage par terre. La borne est une
petite soupape de sûreté délibérée.

Remarque aussi le contexte. Le worker réfléchit avec `a.bgCtx`, un contexte de fond créé
dans `New`, *pas* le contexte par tour. La réflexion doit survivre au tour qui l'a
déclenchée — si elle utilisait le `ctx` du tour, la fin (ou l'annulation) du tour
annulerait l'apprentissage. Hors du chemin critique veut dire hors de la *durée de vie* du
tour, aussi.

## Partie 4 — le contrat d'arrêt : drainer, pas jeter

Un worker de fond soulève une question qu'un appel synchrone n'avait jamais à trancher :
*qu'advient-il de l'apprentissage encore en vol quand le programme s'arrête ?* Le tentant
`go reflect()` a une mauvaise réponse — la goroutine est abandonnée en plein travail, et
tout ce que tu as dit à l'agent juste avant de quitter est silencieusement perdu.

Lis le contrat :

```text
internal/agent/agent.go   (Close, Quiesce)
cmd/talunor/main.go       (defer ag.Close())
```

- `Agent.Close()` **ferme** `reflectCh`, ce qui fait que la boucle `for range` du worker
  finit les jobs encore en file *puis* sort ; `Close` attend ça via `workerWG`. Donc fermer
  l'agent **draine** l'apprentissage en attente au lieu de le jeter. C'est idempotent, et
  ça annule `bgCtx` seulement *après* le drain.
- Dans `cmd/talunor`, `defer ag.Close()` est enregistré **après** `defer store.Close()`,
  donc par LIFO il s'exécute en *premier* — le worker finit d'écrire dans le store avant que
  le store ne se ferme sous lui. L'ordre compte, et l'astuce du defer-LIFO le rend correct.
- `Agent.Quiesce(ctx)` bloque jusqu'à ce que la file soit vide *sans* arrêter le worker.
  C'est ce qui rend le changement testable : un tour rend maintenant la main avant que sa
  réflexion soit finie, donc un test qui inspecte le store doit d'abord attendre que le
  worker rattrape.

La différence entre `go reflect()` et ceci est la différence entre « travail de fond au
mieux » et « travail de fond avec un contrat d'arrêt » — entre un agent qui oublie la
dernière chose que tu as dite et un qui ne l'oublie pas.

## Partie 5 — un travail async ne peut pas narrer un tour clos

Une victime de ce déplacement mérite d'être signalée, car le correctif honnête enseigne
quelque chose. Avant le Layer 18, avec `/debug` activé, la réflexion streamait des notes
grisées dans le transcript (« +fact … », « reinforced … »). Lis `reflect` maintenant :

```text
internal/agent/agent.go   (reflect — remarque qu'il ne prend plus de channel `out`)
```

Il a perdu son paramètre stream. Il *ne peut plus* streamer vers le tour : au moment où le
worker tourne, le canal de ce tour est déjà fermé. Y écrire serait un bug. Donc
l'observabilité de l'étape de réflexion a migré vers le **log structuré** (`a.trace` →
`TALUNOR_DEBUG` fichier/stderr). La trace de *rappel*, qui tourne synchrone avant la
réponse, reste inline ; seule la moitié *réflexion* a bougé.

La leçon : **quand un travail se déplace dans le temps, sa télémétrie doit se déplacer avec
lui.** Plutôt que de contorsionner le cycle de vie pour garder les notes inline (garder le
tour ouvert juste pour narrer l'apprentissage annulerait tout l'intérêt), le geste honnête
est de router l'observabilité du travail différé vers un puits qui survit au tour — et de le
dire clairement.

## Partie 6 — le voir

D'abord les tests — ils encodent les deux garanties :

```bash
go test ./internal/agent/ -run 'CloseDrains|Reflection|Consolidat' -v
go test -race ./internal/agent/
```

Lis `TestCloseDrainsPendingReflection` : il lance un tour, draine le flux de *réponse* (qui
rend la main dès que la réponse est finie), puis appelle `Close()` — et vérifie que le fait
a quand même été stocké. C'est le contrat de drain, épinglé. Le run `-race` est
l'affirmation de la Partie 2, vérifiée : aucune data race malgré un écrivain de fond et des
lectures au premier plan partageant le store.

Maintenant en direct (nécessite Ollama). Envoie la trace de réflexion vers stderr et
observe l'*ordre* :

```bash
TALUNOR_DEBUG=stderr go run ./cmd/talunor --plain
```

```text
you> je m'appelle Carlos et je travaille en Go
```

La réponse revient et l'invite `you>` réapparaît **immédiatement** — tu n'attends pas
l'extraction. Un instant plus tard, une ligne `msg=reflect …` apparaît dans stderr :
l'apprentissage a eu lieu *après* que le tour t'a rendu la main. Puis :

```text
you> /list
```

montre le fait distillé, désormais stocké. Tu viens de voir l'apprentissage sortir du
chemin critique — visible dans le log, invisible dans ton attente.

## Les principes

```text
Déplace le travail lent hors du chemin que l'utilisateur attend — mais donne au travail de fond un verrou qu'il a déjà, et un contrat pour sa fin.
```

1. **Sur le chemin critique = l'utilisateur l'attend.** L'apprentissage ne change pas la
   réponse déjà donnée, donc il ne devrait pas tenir le tour ouvert.
2. **Réutilise le verrou que tu as.** `SetMaxOpenConns(1)` sérialise déjà tout l'accès au
   store ; un écrivain de fond n'a besoin d'aucun mutex de plus. Le worker est pour la
   backpressure, l'ordre et le drain — pas la sûreté.
3. **Le travail de fond a besoin d'un contrat d'arrêt.** Draine la file à `Close`, ne fais
   pas du fire-and-forget ; utilise un contexte de fond pour que la réflexion survive à son
   tour.
4. **La télémétrie suit le travail dans le temps.** Un travail différé ne peut pas narrer un
   tour clos — route son observabilité vers un puits qui survit au tour, et dis-le.

## Checklist de complétion

- [ ] Je peux expliquer ce que « sur le chemin critique » coûtait à l'utilisateur avant le Layer 18.
- [ ] Je peux dire pourquoi la connexion unique épinglée fait qu'aucun verrou de plus n'est
      nécessaire, et le lier à `database/sql` qui sérialise l'accès.
- [ ] Je peux donner les trois vraies raisons de l'existence du worker (backpressure, ordre, drain).
- [ ] J'ai lu `Close`/`Quiesce` et je peux expliquer le contrat de drain et l'ordre defer-LIFO.
- [ ] Je peux expliquer pourquoi la réflexion utilise `bgCtx`, pas le contexte du tour.
- [ ] Je peux expliquer pourquoi les notes `/debug` de la réflexion ont migré vers le log.
- [ ] J'ai lancé l'agent avec `TALUNOR_DEBUG=stderr` et vu la réponse revenir avant `msg=reflect`.

---

## 🎓 À propos de cette leçon

Ceci clôt l'Itération 4 — l'itération de l'*apprentissage* — et le fait avec une leçon de
systèmes plutôt que de mémoire. Suis tout l'arc : un schéma qui peut évoluer (15), des
souvenirs à confiance graduée (16–17), des souvenirs avec une vie (17), et maintenant un
apprentissage qui tourne *quand* il le doit plutôt que de bloquer la conversation (18).
L'agent ne se contente pas de se souvenir davantage ; il se souvient *honnêtement*,
*sélectivement*, et *sans te faire attendre*.

La concurrence ici est délibérément modeste — un worker, un channel borné, un drain — parce
que le but était de sortir le travail du chemin critique *sans* importer tout un framework
de concurrence ni, pire, une data race subtile. La ligne la plus instructive de tout le
layer n'est pas du code du tout : c'est la prise de conscience que la contrainte contre
laquelle tu te battais en Leçon 02 (une connexion unique) s'est révélée être précisément ce
qui a rendu ceci sûr. Relis tes contraintes deux fois — parfois le mur est aussi le sol.

Retour à l'[index du cours](../).
