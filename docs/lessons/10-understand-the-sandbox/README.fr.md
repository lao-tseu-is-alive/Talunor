# Leçon 10 — Comprendre le sandbox

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 4 · **Avancé** · ~90 min

> **Le point d'orgue.** La leçon la plus avancée — elle touche aux internes de Linux.
> Le but n'est *pas* de faire de toi un expert kernel en 90 minutes ; c'est de
> comprendre les **niveaux d'isolation** et, surtout, une valeur d'ingénierie : **ne
> jamais prétendre à plus de sûreté que tu n'en as réellement.**

## Pourquoi cette leçon existe

L'outil `bash` de Talunor peut exécuter *n'importe quelle* commande shell que le modèle
demande. C'est puissant et dangereux, donc ça tourne dans un **sandbox**. Mais
« sandbox » n'est pas une seule chose — c'est un spectre. Talunor livre **deux**
backends et est scrupuleusement honnête sur la force de chacun. Cette honnêteté est la
vraie leçon. Lis-la à **`v0.9.0`**, où le sandbox arrive.

## Objectifs pédagogiques

À la fin tu sais :
- nommer les primitives d'isolation dont un conteneur est fait ;
- expliquer la différence entre les deux backends et quand utiliser chacun ;
- expliquer pourquoi l'un est étiqueté « teaching artifact, not a strong boundary » —
  et pourquoi le dire est une fonctionnalité, pas une faiblesse.

## Prérequis

- Leçons 00–06. C'est le grand bain ; survole ce qui t'est étranger.

## Checkout de la couche sandbox

```bash
git checkout v0.9.0     # detached HEAD — lecture seule (voir Leçon 00)
```

> **Fichiers à ce tag** (la couche d'isolation pour l'outil `bash`) :
>
> ```text
> internal/sandbox/sandbox.go          l'interface Sandbox + Limits + FromEnv
> internal/sandbox/runtime.go          le backend OCI (nerdctl/docker) — le fort
> internal/sandbox/namespaces_linux.go le backend namespaces rootless — le pédagogique
> internal/sandbox/rootfs_linux.go     construit le rootfs busybox dans lequel le conteneur tourne
> internal/sandbox/namespaces_other.go stubs pour non-Linux (pour que ça compile quand même)
> ```

Lis dans cet ordre — l'interface d'abord, puis les deux implémentations :

```text
internal/sandbox/sandbox.go          # le petit contrat : Run(script, Limits)
internal/sandbox/runtime.go          # ociRuntime — délègue à un vrai runtime
internal/sandbox/namespaces_linux.go # celui fait main, rootless (lis les commentaires !)
```

## Deux backends, comparés honnêtement

| Propriété | `ociRuntime` (nerdctl/docker) | `namespaces` (rootless, fait main) |
|-----------|-------------------------------|------------------------------------|
| Dépendance externe | Oui (un runtime de conteneur) | Non — du Go pur + le kernel |
| Force d'isolation | **Forte** | **Limitée** |
| seccomp (filtre de syscalls) | Oui (du runtime) | **Non** |
| Idéal pour | du code réellement non fiable | *apprendre ce que fait un runtime* |

Le backend `namespaces` réexécute le propre binaire de Talunor comme « init » de
conteneur dans de fraîches namespaces **user / mount / pid / net**, fait un `pivot_root`
vers un système de fichiers busybox en lecture seule, drop les capabilities, pose
`no_new_privs`, et lui donne une namespace réseau vide (donc : pas de réseau). Ça
*ressemble* à un conteneur — parce qu'il fait, à la main, ce qu'un runtime fait pour toi.

## Le cœur de la leçon : des frontières honnêtes

Trouve le commentaire dans `namespaces_linux.go` qui dit, en substance : *« il n'y a pas
de filtre seccomp, donc toute la surface de syscalls Linux est atteignable — c'est de la
défense en profondeur et un artefact pédagogique, pas une frontière pour du code
hostile. »*

Reste avec ça un moment. L'auteur a construit un sandbox impressionnant **puis t'a dit de
ne pas lui faire confiance pour de vraies menaces.** C'est l'inverse de la plupart du
théâtre sécuritaire.

> Un mécanisme complexe ne doit jamais être présenté comme une garantie plus forte qu'il
> ne l'est réellement. Nommer la limite (pas de seccomp → pas une vraie frontière) est ce
> qui transforme une démo en ingénierie digne de confiance. Quand ça compte, le code
> dit : utilise le backend OCI.

C'est l'idée la plus importante de tout le cours : **la valeur d'un garde-fou est
indissociable d'un compte-rendu honnête de là où il s'arrête.**

## Expérience

Compare les deux chemins par la lecture, et (optionnellement) lance les tests du sandbox :

```bash
go test ./internal/sandbox/ -v   # certains cas se skippent si l'hôte ne peut fournir un backend
```

Lancer l'outil `bash` pour de vrai nécessite une mise en place (`TALUNOR_BASH=1`, un
runtime ou des user namespaces non privilégiées ; sur Ubuntu 24.04 un toggle AppArmor —
voir le helper dans `scripts/` et le `README.md` sur `main`). Si ton hôte l'autorise :

```bash
git switch main
TALUNOR_BASH=1 go run ./cmd/talunor --plain
# demande-lui de lancer : id ; pwd ; ls /   — observe le peu de l'hôte qui est visible
```

Puis reviens au code le plus récent :

```bash
git switch main
```

## Questions auxquelles répondre

- Nomme trois namespaces que le sandbox utilise et ce que chacune cache.
- Pourquoi une namespace réseau vide signifie-t-elle « pas de réseau » ?
- Pourquoi le backend `namespaces` n'est-il *pas* recommandé pour du code réellement non
  fiable, et que devrais-tu utiliser à la place ?
- Pourquoi documenter une faiblesse est-il un signe de *bon* travail de sécurité, pas
  mauvais ?

## Erreurs fréquentes

- **Lire `namespaces_linux.go` en premier.** Commence par l'interface et le backend OCI ;
  celui fait main n'a de sens qu'une fois que tu sais ce qu'il imite.
- **Croire que la démo est inviolable** parce qu'elle ressemble à un conteneur. Le code
  lui-même dit le contraire — crois le code.

## Checklist de complétion

- [ ] Je peux nommer les deux backends et quand utiliser chacun.
- [ ] Je peux lister quelques primitives d'isolation (namespaces user/mount/pid/net,
      pivot_root, caps).
- [ ] J'ai trouvé le commentaire « no seccomp / teaching artifact » et je peux expliquer
      pourquoi cette honnêteté compte.
- [ ] Je peux énoncer l'idée du point d'orgue : la valeur d'un garde-fou inclut un
      compte-rendu honnête de ses limites.
- [ ] Je suis revenu à `main`.

---

## 🎓 Tu as terminé le cours

Tu as parcouru Talunor d'un simple magasin de mémoire jusqu'à un agent complet avec des
outils, des tests et une sécurité honnête. Tu peux maintenant :

1. le lancer ; 2. expliquer son architecture ; 3. suivre un tour d'agent ; 4. ajouter un
outil ; 5. écrire un test déterministe ; 6. raisonner sur ses limites de sécurité ;
7. justifier au moins un compromis de conception.

Talunor n'est plus seulement *conçu* comme un projet pédagogique — pour toi, il est
devenu un cours pratique de **Go, d'agents IA, de testabilité et de code sûr par
conception**.

Retour à l'[index du cours](../).

> **Encore une, tirée du réel.** La Leçon 10 est l'apogée du programme prévu, mais le
> cours continue de grandir avec le projet. **[Leçon 11 — Quand la mémoire oublie en
> silence](../11-when-memory-forgets/)** dissèque un *vrai* bug corrigé dans
> l'histoire de Talunor : un rappel qui renvoyait discrètement les mauvais souvenirs
> après un changement de modèle d'embeddings. Un rappel bienvenu de la façon dont les
> systèmes en production échouent *en silence*.
