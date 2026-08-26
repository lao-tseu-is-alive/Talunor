# Leçon 20 — Apprendre de l'action : la plupart du « savoir des outils » est *interprété* par le modèle

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration + pratique** (lecture de `internal/agent`, `internal/memory`,
`internal/tools` ; le code de la Couche 20 est livré à `v0.19.0`, docs de référence sur
`main`) · Niveau 3 (avancé) · ~65 min

## Pourquoi cette leçon existe

Pendant toute l'Itération 4, l'agent n'a appris que d'une seule chose : **ce que tu as
dit**. Chaque fait stocké par la réflexion était étiqueté `user_stated`. Mais un agent qui
*agit* *observe* aussi — il récupère une page, lance une commande, reçoit un résultat
d'outil. Ne devrait-il pas apprendre de ça aussi ?

Oui — et le point intéressant, c'est *à quel point honnêtement*. La conception tentante
est : « ça vient d'un outil, les outils sont fiables, marquons-le `tool_observed` ». Ce seul
mot blanchit discrètement une supposition du modèle en confiance élevée. La Couche 20 ouvre
l'Itération 5 (« mémoire véridique ») en apprenant de l'action **sans** cette malhonnêteté,
et la discipline que ça exige est toute la leçon : **la provenance doit venir de la source,
pas de l'empressement.**

## Objectifs d'apprentissage

À la fin, tu sais :
- expliquer pourquoi un fait que le modèle distille de la sortie texte d'un outil est
  `model_inferred`, et non `tool_observed` — et ce qu'est vraiment le cas étroit `tool_observed` ;
- expliquer pourquoi garder la confiance *assignée par le système* impose une extraction
  par source (et pourquoi le « un seul appel, le modèle étiquette chaque fait » est le piège) ;
- lire le `reflect` / `learnFrom` multi-sources et nommer ce qui décide la provenance de chaque fait ;
- expliquer la piste d'évidence (migration 4) et utiliser `/why` pour l'inspecter ;
- dire pourquoi une capacité qu'aucun outil livré n'utilise (`tools.Verified`) peut valoir la peine.

## Prérequis

- **Leçon 16** (provenance & confiance) — d'où viennent les niveaux de `Provenance` et la
  confiance assignée par le système. Cette leçon *peuple* enfin les niveaux inutilisés.
- **Leçon 17** (apprendre avec humilité) — la règle d'indépendance (`EvidenceCredibility`)
  est ce qui garde la confiance honnête ici aussi.
- **Leçon 18** (hors du chemin critique) — la réflexion que la Couche 20 élargit est le
  worker asynchrone de cette leçon.

## Partie 1 — le mensonge tentant

Lis d'où les faits tirent leur provenance aujourd'hui, sur `main` :

```text
internal/memory/memory.go   (Provenance, BaseConfidence)
```

Il y a quatre niveaux — `user_stated`, `tool_observed`, `model_inferred`, `unspecified` —
et la confiance est assignée par le *système* à partir du niveau (un outil vérifié > l'utilisateur
> le modèle). Avant la Couche 20, seul `user_stated` était jamais produit par la réflexion ;
les autres étaient définis mais dormants.

Maintenant la tentation. L'agent lance `web_fetch`, la page dit « La population de la France
est de 68 millions », et le modèle distille « La population de la France est de 68 M ».
Quelle provenance ?

« Ça vient d'un outil → `tool_observed` → confiance 0,95 » **semble** juste et **est faux**.
L'outil a renvoyé du *texte*. Un LLM a *lu* ce texte et produit un fait. La lecture est de
l'interprétation, et l'interprétation est de l'inférence — le modèle a pu mal lire,
halluciner un nombre, ou se fier à une mauvaise page. Étiqueter ça `tool_observed` donne à
une supposition du modèle l'autorité d'une mesure vérifiée. C'est le piège à sycophantie
contre lequel la Leçon 16 mettait en garde, portant l'uniforme d'un outil.

## Partie 2 — la règle honnête (ADR 0002)

Lis la décision et la capacité sur laquelle elle repose :

```text
docs/decisions/0002-provenance-from-source.md
internal/tools/tool.go   (l'interface Verified)
```

La règle qu'adopte la Couche 20 :

> Un fait que le LLM distille de la sortie **texte** d'un outil est **`model_inferred`**. Un
> fait est **`tool_observed`** seulement quand il vient d'un outil qui déclare sa sortie un
> fait déterministe et structuré qu'il affirme directement — la capacité optionnelle
> `tools.Verified` (`Verified() bool`).

Et la chute honnête : **aucun outil livré n'implémente `Verified`.** La calculatrice et
l'horloge sont déterministes mais ne produisent rien de durable (« 2+2=4 » n'est pas un fait
à retenir) ; `web_fetch` et `bash` renvoient de la prose que le modèle doit interpréter. Donc
en pratique la Couche 20 peuple `model_inferred`, et `tool_observed` est une **couture
câblée et testée** attendant un futur outil qui renverrait des faits vérifiés durables.

Une capacité que rien n'utilise vaut-elle la peine ? Ici, oui — parce que c'est une *couture
testée qui résiste à un mauvais défaut*. Sans elle, la prochaine personne ajoutant un outil
qui renvoie des données structurées serait tentée d'attraper « `tool_observed`, ça vient d'un
outil ». La capacité rend le chemin honnête évident : on n'obtient `tool_observed` qu'en
*déclarant* la vérification, exprès.

## Partie 3 — pourquoi la provenance doit être assignée par source

Voici la contrainte de conception qui façonne le flux de contrôle, et elle mérite qu'on ralentisse.

La confiance est **assignée par le système** (Leçon 16) : on ne demande jamais au modèle à
quel point il est sûr, parce que la confiance auto-reportée d'un modèle n'est pas calibrée.
Étends ça : il ne faut pas non plus demander au modèle d'étiqueter sa propre *provenance*.
Dès l'instant où tu demandes « lesquels de ces faits viennent de l'utilisateur, d'un outil,
de ton propre raisonnement ? », le modèle s'auto-reporte — et il appellera volontiers sa
propre inférence « observée ».

Donc le **système** doit connaître la source de chaque fait. Et la seule façon pour le
système de la connaître est de garder les sources *séparées* : extraire du message
utilisateur, étiqueter les résultats `user_stated` ; extraire d'une observation d'outil,
étiqueter ceux-là `model_inferred` ; ne jamais les mélanger dans un seul appel en demandant
au modèle de trier.

Lis comment `reflect` fait exactement ça :

```text
internal/agent/learn.go   (reflect, learnFrom, toolVerified, worthReflecting)
internal/agent/reflect.go (le prompt d'extraction neutre vis-à-vis de la source)
```

- `reflect(job)` boucle sur les **sources** du tour : le message utilisateur, chaque
  observation d'outil, et — seulement si `Config.ReflectAssistant` est activé (désactivé par
  défaut) — la réponse de l'assistant.
- Pour chaque source, il appelle `learnFrom(text, prov, turnID)` avec la provenance que le
  *système* a choisie pour cette source. L'extracteur lui-même est neutre (« trouve des faits
  durables dans ce texte ») ; il ne décide pas, et on ne lui demande jamais de décider, la provenance.
- `toolVerified(name)` est la seule chose qui peut faire monter une observation à
  `tool_observed`, et elle le fait par une assertion de type `tools.Verified` — un fait
  structurel sur l'outil, pas une opinion du modèle.

L'alternative bon marché (un seul appel d'extraction sur toutes les sources, le modèle
étiquetant chaque fait) serait moins de tokens et *fausse* : elle rend la provenance au
modèle. La forme par source, plus verbeuse, est ce qui garde intact l'invariant de la
Leçon 16. **La règle d'honnêteté a dicté le flux de contrôle.**

Note les deux gardes qui gardent l'apprentissage supplémentaire bon marché :
`worthReflecting` saute les observations vides, en erreur, et des *outils triviaux* (les
sorties calculatrice/horloge/recall-memory qui ne portent jamais un fait durable), et le
texte de l'observation est plafonné en taille avant extraction. Tout cela roule sur le seul
interrupteur `TALUNOR_REFLECT` et le worker asynchrone de la Leçon 18 — aucun nouveau réglage.

## Partie 4 — la piste d'évidence, et « pourquoi crois-tu ça ? »

Un fait porte désormais une provenance *et* une confiance, mais pas *d'où il vient*. La
Couche 20 ajoute la piste d'audit :

```text
internal/memory/migrate.go   (migration 4)
internal/memory/evidence.go  (RecordEvidence, EvidenceFor, MemoryByID)
```

La migration 4 (append-only, aucun changement à la table `memories`) ajoute une table
`evidence`. Chaque fois que `learnFrom` **stocke** un nouveau fait ou **renforce** un fait
existant, elle ajoute une ligne : quel fait, quel tour, de quelle source. Ainsi un fait
reformulé sur trois tours a trois lignes d'évidence — le registre de *comment la croyance
s'est accumulée*.

C'est ce qui rend l'agent redevable. Lis `WhyMemory` et essaie (`/why <id>`) : au lieu de
« je crois X (90 %) », tu obtiens « je crois X parce que l'utilisateur l'a dit aux tours #3
et #9 ». C'est aussi la matière première que la Couche 21 arbitrera : pour décider si un
nouveau fait devrait *remplacer* un ancien, il faut savoir ce qui soutenait l'ancien.

## Partie 5 — regarde-le

Les tests figent les deux garanties — provenance honnête et piste d'évidence :

```bash
go test ./internal/memory/ -run 'Evidence|MemoryByID' -v
go test ./internal/agent/ -run 'LearnsFromToolObservation' -v
```

Lis `TestReflectLearnsFromToolObservation` : il lance un tour où le modèle appelle un faux
outil, et vérifie que le fait distillé de la sortie de l'outil est `model_inferred` quand
l'outil est non vérifié et `tool_observed` quand il déclare `Verified()` — avec une ligne
d'évidence de la bonne source, et `/why` qui la fait apparaître. C'est la Partie 2 et la
Partie 4, figées.

Maintenant en direct (nécessite Ollama). Le nouveau comportement toujours actif est la
piste d'évidence :

```bash
go run ./cmd/talunor --plain
```
```text
you> je m'appelle Ada et je travaille surtout en Rust
you> /list
you> /why <le #id du fait « User's name is Ada »>
```

`/why` montre une ligne d'évidence `user_stated` ancrée à ton tour. Pour voir le chemin des
outils, active l'opt-in réseau et demande un fetch :

```bash
TALUNOR_WEBFETCH=1 TALUNOR_DEBUG=stderr go run ./cmd/talunor --plain
```

Observe la trace sur stderr : un fait distillé de la page récupérée est stocké comme
`model_inferred` — jamais `tool_observed`, parce que `web_fetch` ne se déclare pas vérifié.
Tu as regardé l'agent apprendre de ce qu'il a *fait*, honnêtement.

## Les principes

```text
Apprends de ce que tu observes — mais laisse la SOURCE, pas l'empressement du modèle, fixer la confiance.
```

1. **L'interprétation est de l'inférence.** Un fait que le modèle lit dans le texte d'un
   outil est `model_inferred`. `tool_observed` est le cas étroit d'un outil qui *affirme* un
   fait vérifié — une couture, honnêtement étiquetée, pas un défaut.
2. **La provenance assignée par le système impose l'extraction par source.** Si le modèle
   étiquette sa propre provenance, la confiance n'est plus assignée par le système. Garde les
   sources séparées pour que le *système* connaisse l'origine de chaque fait.
3. **Enregistre l'évidence, pas seulement la croyance.** Une piste d'audit (quels tours,
   quelles sources) rend l'agent redevable — et c'est ce qu'un futur pas de correction arbitre.
4. **Une couture testée peut valoir plus qu'une couture utilisée.** `tools.Verified` ne se
   déclenche pour aucun builtin aujourd'hui ; elle existe pour que le chemin honnête soit
   l'évident quand un outil à faits durables arrivera.

## Checklist de complétion

- [ ] Je sais expliquer pourquoi un fait distillé d'un résultat `web_fetch` est `model_inferred`, pas `tool_observed`.
- [ ] Je sais dire ce qu'est le cas étroit `tool_observed`, et pourquoi aucun builtin ne le déclenche encore.
- [ ] Je sais expliquer pourquoi garder la confiance assignée par le système impose d'extraire les sources séparément.
- [ ] J'ai lu `reflect`/`learnFrom` et je sais nommer ce qui fixe la provenance de chaque fait.
- [ ] J'ai lancé `/why <id>` et vu la piste d'évidence d'un fait (tours + sources).
- [ ] Je sais argumenter pourquoi `tools.Verified` vaut la peine d'être livré même si rien ne l'implémente.

---

## 🎓 À propos de cette leçon

Ceci ouvre l'Itération 5 — « mémoire véridique » — et l'ouvre avec retenue. Toute la couche
aurait pu être « l'agent apprend tout ce qu'il voit, et le croit », ce qui démontre bien et
pourrit en silence : une mémoire pleine de faits confiants-mais-faux glanés sur des pages. À
la place, la couche apprend de l'action tout en *sous-estimant* — le défaut est le niveau
humble, et le niveau confiant est une porte qu'il faut ouvrir exprès.

Cette retenue est le fil rouge de tout l'arc d'apprentissage. La Leçon 16 a refusé de
laisser le modèle noter sa propre confiance ; la Leçon 17 a refusé de laisser le modèle se
corroborer lui-même ; cette leçon refuse de laisser l'*uniforme* d'un outil tenir lieu de sa
*vérification*. Chaque fois, le choix honnête était le plus verbeux, et chaque fois il en
valait la peine. Un agent en qui tu peux avoir confiance n'est pas celui qui en sait le plus
— c'est celui dont tu peux *rendre compte* de la confiance. La piste d'évidence est là où ce
rendu de compte devient enfin quelque chose que tu peux lire.

Ensuite, l'Itération 5 passe d'*apprendre plus véridiquement* à *rester vrai* : la
**Leçon 21** laissera un nouveau fait **remplacer** un ancien — parce qu'une mémoire qui
peut accumuler et oublier, mais jamais *corriger*, n'apprend pas vraiment.

Retour à l'[index du cours](../README.fr.md).
