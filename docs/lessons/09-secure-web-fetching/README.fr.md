# Leçon 09 — Récupération web sécurisée (SSRF)

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** · Niveau 3 · **Avancé** · ~75 min

> **Leçon de sécurité avancée.** Elle récompense les Leçons 04–05. Prends ton temps.

## Pourquoi cette leçon existe

Donne à un agent le pouvoir de récupérer une URL et tu as ouvert une porte : un
attaquant peut lui demander de récupérer une URL qui pointe **vers l'intérieur** — ta
base de données, le panneau d'admin d'un voisin, ou le endpoint de métadonnées cloud
qui distribue des identifiants. C'est le **SSRF** (Server-Side Request Forgery).
L'outil `web_fetch` de Talunor est conçu pour refuser ces destinations, et *la manière*
dont il les refuse est un vrai bon morceau d'ingénierie de sécurité. Tu le liras à
**`v0.10.0`**, où il apparaît pour la première fois.

## Objectifs pédagogiques

À la fin tu sais :
- expliquer ce qu'est le SSRF et pourquoi une allowlist d'URL ne suffit pas ;
- expliquer pourquoi vérifier l'IP **au moment de la connexion** vaut mieux que
  résoudre-puis-vérifier ;
- lire un garde de sécurité écrit comme une *fonction pure, testée en table*.

## Prérequis

- Leçons 04–05. Aisance à lire du Go.

## Checkout de la couche web_fetch

```bash
git checkout v0.10.0     # detached HEAD — lecture seule (voir Leçon 00)
```

> **Fichiers à ce tag** (l'outil « opt-in réseau ») :
>
> ```text
> internal/webfetch/webfetch.go       le fetcher gardé : blockedIP, guardDial, Fetch
> internal/webfetch/webfetch_test.go  la table du classifieur SSRF + le test de redirection
> internal/tools/webfetch.go          le wrapper outil (schéma, allowlist, approbation)
> ```

Lis, dans cet ordre :

```text
internal/webfetch/webfetch.go       # blockedIP → guardDial → checkRedirect → Fetch
internal/webfetch/webfetch_test.go  # TestBlockedIP, TestFetchRedirectToInternalBlocked
```

## L'idée en trois niveaux

**Intuition.** Ton agent peut récupérer une page web. Un attaquant lui donne une URL
pointant *à l'intérieur* de ton réseau — un service privé, ou `169.254.169.254`,
l'adresse cloud qui distribue des identifiants. Tu dois refuser ça.

**Technique.** Le garde naïf est : résoudre le nom d'hôte en une IP, vérifier l'IP,
puis se connecter. Mais entre la *vérification* et la *connexion*, un serveur DNS
hostile peut changer sa réponse en une IP interne — le **DNS rebinding**. La parade :
vérifier l'IP **au moment exact de la connexion**, sur l'IP réellement composée — et le
faire à chaque saut de redirection, puisqu'une URL publique peut te faire un `302` vers
une interne.

**Dans Talunor.** La vérification vit dans le hook `Control` du dialer — lis
`guardDial` : il reçoit l'`ip:port` résolu *juste avant* `connect()`, donc l'IP vérifiée
est l'IP composée, toujours. La décision elle-même est une **fonction pure**,
`blockedIP`, qui refuse le loopback, le privé (RFC1918), le link-local (dont
`169.254.169.254`), le CGNAT, et plus — et **échoue fermé** sur tout ce qu'elle ne peut
pas classer. Parce qu'elle est pure, elle est testée exhaustivement dans une table
(`TestBlockedIP`) sans aucun réseau.

## Le test décisif

Ouvre `TestFetchRedirectToInternalBlocked`. Il monte un serveur de test normal
(loopback) qui répond par une redirection vers `http://169.254.169.254/…`, et vérifie
que le fetch est **refusé**. Ça prouve que le garde re-vérifie *après* une redirection —
le bypass SSRF classique — pas seulement à la première requête.

## Expérience

Le garde est testable sans réseau, c'est tout l'intérêt :

```bash
go test ./internal/webfetch/ -v
```

Regarde `TestBlockedIP` parcourir une table d'adresses (publique → autorisée, interne →
bloquée) et `TestFetchRedirectToInternalBlocked` prouver le cas de la redirection. Puis
lis quelques lignes de la table et prédis le résultat avant de le voir.

Reviens au code le plus récent une fois fini :

```bash
git switch main
```

## Extension 🛠️ optionnelle (sur `main`)

Le garde bloque `0.0.0.0` exactement, mais pas tout `0.0.0.0/8` (la plage « ce réseau »,
qui peut se comporter comme du loopback sur certains systèmes). Comme petit durcissement,
branche depuis `main` et ajoute `0.0.0.0/8` aux CIDR bloqués, avec une nouvelle ligne
dans `TestBlockedIP` :

```bash
git switch main && git pull && git switch -c learning/harden-blockedip
# puis : ajoute "0.0.0.0/8" aux plages bloquées + un cas de test
go test ./internal/webfetch/ -run BlockedIP -v
```

C'est de la défense en profondeur : une *fonction pure* est l'endroit le plus facile
d'un code pour ajouter une règle et la prouver.

## Questions auxquelles répondre

- Pourquoi une **allowlist** de domaines n'est-elle pas une défense SSRF complète à
  elle seule ? (Indice : que peuvent faire une redirection, ou un enregistrement DNS
  hostile pour un domaine allowlisté ?)
- Pourquoi `blockedIP` est-elle une fonction pure plutôt qu'une méthode qui ouvre une
  connexion ?
- Que signifie « échouer fermé » ici, et pourquoi est-ce le bon défaut pour un garde ?

## Erreurs fréquentes

- **Confondre l'allowlist et le garde.** Dans `tools/webfetch.go`, une allowlist peut
  sauter le *prompt d'approbation* — mais elle ne saute jamais le garde IP. Un hôte
  « autorisé » qui résout vers une IP interne est quand même refusé.
- **Croire que le filtrage IP est toute l'histoire.** Le *timing* (vérifier au connect,
  re-vérifier par redirection) compte autant que la liste.

## Checklist de complétion

- [ ] Je peux expliquer le SSRF et donner une vraie cible (ex. le endpoint de métadonnées).
- [ ] Je peux expliquer pourquoi vérifier-au-moment-du-connect défait le DNS rebinding.
- [ ] J'ai lu `blockedIP` et `guardDial` et je peux dire comment ils s'articulent.
- [ ] J'ai lancé les tests webfetch et compris le test de redirection.
- [ ] Je suis revenu à `main`.

**Suivant :** [Leçon 10 — Comprendre le sandbox](../10-understand-the-sandbox/) — le
point d'orgue du cours.
