# Leçon 22 — La suite silencieuse : un test sauté n'est pas un test réussi

**Langue :** [🇬🇧 English](README.md) · 🇫🇷 Français

**🔍 Exploration historique** (le trou à `v0.20.1`, le correctif à `v0.20.2`) ·
Niveau 3 (avancé) · ~70 min

## Pourquoi cette leçon existe

La sandbox de Talunor repose sur une astuce bien documentée : pour exécuter un script
isolé, elle **ré-exécute son propre binaire** (`/proc/self/exe`) et laisse un hook
`init()` transformer cet enfant en init de conteneur. La leçon 10 t'a appris l'astuce.
Ce qu'elle n'a pas dit, c'est que le hook est armé par une *variable d'environnement* —
et une variable d'environnement est héritée par tous les descendants de celui qui l'a
exportée.

Un relecteur l'a remarqué. Un correctif a été écrit. Il était bien raisonné, bien
commenté, et **il désactivait complètement la sandbox** — pendant que la suite de tests
restait verte.

C'est cette dernière proposition, la leçon. Pas « l'IA écrit des bugs » (la leçon 15
traite déjà de vérifier ce qu'une machine affirme sur ton code), mais quelque chose qui
vaut pour tout correctif que tu reliras un jour, humain ou non : **une suite de tests te
dit ce qu'elle a exécuté, et rien ne l'oblige à te dire ce qu'elle n'a pas exécuté.**
Quatre tests de ce paquet sautaient silencieusement depuis des semaines.

## Objectifs d'apprentissage

À la fin, tu sais :
- mesurer la gravité *réelle* d'un défaut avant de le corriger, au lieu de celle
  qu'annonce son rapport ;
- expliquer ce qui authentifie un processus enfant ré-exécuté — et pourquoi une variable
  d'environnement seule ne le peut jamais ;
- nommer trois modes de défaillance de l'implémentation « évidente » : un fd fermé avant
  qu'`exec.Cmd` ne le duplique, une comparaison que deux chaînes vides satisfont, et une
  lecture non bornée sur un descripteur que tu n'as pas ouvert ;
- distinguer une garde à la **compilation** (`//go:build`) d'une garde à
  l'**exécution** (`if runtime.GOOS != "linux"`) et dire laquelle il faut à un test ;
- auditer ta propre suite à la recherche de sauts silencieux, et concevoir un test pour
  que la décision importante reste vérifiable sur une machine incapable de faire tourner
  la fonctionnalité.

## Prérequis

- **Leçon 10 (la sandbox)** — le ré-exec `/proc/self/exe`, les deux backends.
- **Leçon 07 (tester sans vrai LLM)** — doublures et déterminisme ; cette leçon en est
  la suite inconfortable.
- Utile : **leçon 15**, pour le réflexe de vérifier une affirmation avant d'agir dessus.

## Partie 1 — vérifie le constat avant de corriger

Le rapport disait : *« si un utilisateur avait accidentellement `TALUNOR_SANDBOX_CHILD=1`
dans son shell, tout binaire important `internal/sandbox` appellerait `childMain()` dans
`init()` et tenterait un `pivot_root` sur l'hôte. »*

La moitié est vraie. Trouve laquelle — depuis `main`, sans aucun privilège :

```bash
go test -c -o /tmp/sandbox.test ./internal/sandbox/
TALUNOR_SANDBOX_CHILD=1 /tmp/sandbox.test -test.run TestFromEnvUnknown ; echo "exit=$?"
```

À `v0.20.1`, cela affiche :

```
sandbox child: make / private: operation not permitted
exit=127
```

Lis-le attentivement, car cela tranche la gravité :

- **Confirmé :** le détournement est réel, et il atteint *tout* binaire liant le paquet —
  `cmd/talunor` (via `internal/tools`) et chaque binaire de test. Le processus sort en
  127 **avant que `main()` ne tourne**, en accusant un montage que l'utilisateur n'a
  jamais demandé.
- **Exagéré :** il n'approche jamais `pivot_root`. `childMain` meurt au *premier* appel
  système de `setupRoot` — `mount(MS_REC|MS_PRIVATE, "/")` — avec `EPERM`, car un
  processus ordinaire n'a pas `CAP_SYS_ADMIN` dans son espace de noms de montage.

C'est donc un **piège / auto-déni de service**, pas une voie pour saccager les montages
de l'hôte. À corriger — un diagnostic qui dit « operation not permitted » quand le vrai
problème est une variable qui traîne coûte un après-midi à quelqu'un — mais à corriger
*au bon niveau de priorité*, avec les bons mots dans le changelog. Mesurer d'abord n'est
pas du pédantisme : c'est ce qui garde une note de sécurité honnête.

## Partie 2 — le correctif proposé (trouve trois défauts)

La *conception* du correctif était juste, et vaut d'être apprise pour elle-même. Deux
faits indépendants doivent garder l'enfant :

1. **pid == 1.** Le vrai enfant est pid 1 de son propre espace de noms `CLONE_NEWPID`.
2. **Un secret par exécution.** Le parent tire un jeton aléatoire, l'envoie par un
   **tube** hérité en fd 3, et le répète dans une variable d'environnement. Un attaquant
   peut exporter la variable ; il ne peut pas écrire dans notre tube.

Voici le côté parent tel que proposé. **Avant de lire la suite, cherche ce qui le
casse :**

```go
tokenR, tokenW, err := os.Pipe()
cmd := exec.CommandContext(runCtx, "/proc/self/exe")
cmd.ExtraFiles = append(cmd.ExtraFiles, tokenR)
cmd.Env = append(os.Environ(), envChild+"=1", envToken+"="+tokenStr /* … */)

_, _ = tokenW.WriteString(tokenStr)
tokenW.Close()
tokenR.Close()          // « le parent ne lit jamais dans le tube »
err = cmd.Run()
```

et le côté enfant :

```go
if os.Getpid() != 1 {
    die(...)
}
tokenFD := os.NewFile(3, "sandbox-token")
tokenBytes, err := io.ReadAll(tokenFD)
if err != nil {
    die(...)
}
if string(tokenBytes) != os.Getenv(envToken) {
    die(errors.New("token mismatch"))
}
```

Trois défauts. Prends une minute avant la partie 3.

## Partie 3 — défaut 1 : `ExtraFiles` est dupliqué au `Start`, pas à l'affectation

`cmd.ExtraFiles = append(…, tokenR)` ne transmet rien à personne. `os/exec` ne duplique
ces descripteurs dans l'enfant qu'au **démarrage** du processus — à l'intérieur de
`cmd.Run()`. Fermer `tokenR` à la ligne précédente ferme donc le fd que l'enfant devait
hériter.

Prouve-le en vingt lignes, hors du projet (pas de cgo, pas de dépôt, rien à installer) :

```bash
mkdir /tmp/fdlab && cd /tmp/fdlab && go mod init fdlab
cat > main.go <<'EOF'
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func main() {
	if os.Getenv("CHILD") == "1" {
		got := make([]byte, 8)
		_, err := io.ReadFull(os.NewFile(3, "token"), got)
		fmt.Printf("child: got=%q err=%v\n", got, err)
		return
	}
	r, w, _ := os.Pipe()
	w.WriteString("TOKEN123")
	w.Close()
	cmd := exec.Command(os.Args[0])
	cmd.ExtraFiles = []*os.File{r}
	cmd.Env = append(os.Environ(), "CHILD=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	r.Close()                    // ← le bug : déplace cette ligne après cmd.Run()
	fmt.Println("parent:", cmd.Run())
}
EOF
go run .
```

```
child: got="\x00\x00\x00\x00\x00\x00\x00\x00" err=read token: bad file descriptor
```

Déplace maintenant `r.Close()` après `cmd.Run()` et relance : `got="TOKEN123"`. Le cycle
de vie correct est asymétrique et mérite d'être mémorisé — l'extrémité **d'écriture** se
ferme *tôt* (pour que l'enfant voie EOF après le jeton), l'extrémité de **lecture** se
ferme *tard* (pour que `Start` puisse la dupliquer) :

```go
defer tokenR.Close()                  // après Run, via defer
if _, err := tokenW.WriteString(token); err != nil { /* … */ }
if err := tokenW.Close(); err != nil { /* … */ }   // avant Run
```

Dans Talunor ce défaut n'est pas cosmétique : toute exécution légitime de la sandbox
échoue au contrôle du jeton avec `bad file descriptor`, donc `bash` est mort. Le parent
voit `cmd.Run()` renvoyer une simple erreur de sortie 127 et la rapporte comme la sortie
du *script*, exactement le genre d'échec qu'un lecteur pressé appelle « la sandbox est
cassée sur cette machine ».

## Partie 4 — défaut 2 : deux chaînes vides sont égales

```go
if string(tokenBytes) != os.Getenv(envToken) { die(...) }
```

Suppose que fd 3 soit ouvert mais renvoie EOF immédiatement — `/dev/null`, ou un tube que
quelqu'un a déjà vidé. `io.ReadAll` renvoie `""`. Suppose que `TALUNOR_SANDBOX_TOKEN` ne
soit pas définie : `os.Getenv` renvoie `""`. `"" == ""`, et l'imposteur franchit la
barrière construite pour l'arrêter. La seconde garde est *vide de sens* précisément dans
la situation pour laquelle elle existe.

La règle dépasse largement ce fichier :

> **Authentifie la forme avant de comparer la valeur.** Un secret absent, vide ou de
> mauvaise longueur doit être rejeté *comme malformé* — jamais confié à un test d'égalité
> qu'un attaquant peut satisfaire en ne fournissant rien du tout.

Lis la version corrigée (`verifyChildIdentity`, `internal/sandbox/namespaces_linux.go`) :
le contrôle de longueur vient en premier, et la comparaison est
`subtle.ConstantTimeCompare`.

```go
if len(envValue) != tokenHexLen {
    return fmt.Errorf("%s is missing or malformed: not a sandbox child", envToken)
}
```

Son test de non-régression est le cas qui, sinon, aurait été livré :

```go
{
    name: "pid 1, empty token, fd at EOF",
    pid:  childInitPID, fd: devNull, env: "",
    wants: envToken + " is missing",
},
```

## Partie 5 — défaut 3 : le fd 3 ne t'appartient pas

`io.ReadAll(os.NewFile(3, …))` lit **ce que fd 3 se trouve être** dans un processus qui
n'attendait pas cette astuce. Un superviseur, un shell, un runtime de langage — n'importe
quoi a pu y laisser quelque chose. Deux mauvaises issues :

- fd 3 est un fichier ordinaire ou une socket : la garde *consomme* des données qui
  appartiennent à quelqu'un d'autre ;
- fd 3 est un tube ou une socket avec un écrivain vivant : `ReadAll` bloque **pour
  toujours**, dans `init()`, là où aucun logger n'est configuré et où aucun délai
  n'existe. Le binaire se fige avant `main()` sans la moindre sortie. C'est strictement
  pire que le bug qu'on corrigeait.

Le contrôle du pid suffit-il à rendre cela inatteignable ? Presque — et ce « presque »
est justement pourquoi le détail compte : **Talunor est pid 1 dans sa propre image
Docker.** Là, la garde 1 passe et tout repose sur la garde 2.

Le correctif déclare ce qu'il accepte de lire, puis lit exactement cette quantité :

```go
var st unix.Stat_t
if err := unix.Fstat(int(tokenFD.Fd()), &st); err != nil { /* … */ }
if st.Mode&unix.S_IFMT != unix.S_IFIFO {
    return fmt.Errorf("fd %d is not a pipe: not a sandbox child", childTokenFD)
}
got := make([]byte, tokenHexLen)
if _, err := io.ReadFull(tokenFD, got); err != nil { /* … */ }
```

### Un quatrième, offert : un `if` d'exécution ≠ une garde de compilation

Le test du correctif s'ouvrait sur `if runtime.GOOS != "linux" { t.Skip(...) }` puis
utilisait `envChild` — une constante qui n'existe que dans `namespaces_linux.go`. Un
`t.Skip` s'exécute *après* la compilation ; le fichier doit quand même compiler partout :

```bash
GOOS=darwin go vet ./internal/sandbox/
# vet: sandbox_test.go:170:3: undefined: envChild
```

`namespaces_other.go` existe précisément pour que ce paquet continue de compiler hors
Linux. Le correctif place les nouveaux tests dans `namespaces_linux_test.go` avec
`//go:build linux` en tête. **Une garde d'exécution protège l'exécution ; seule une
balise de build protège la compilation.**

## Partie 6 — la vraie question : pourquoi personne n'a rien vu ?

Trois défauts, dont un fatal à la fonctionnalité, et :

```bash
go test ./internal/sandbox/
ok  	github.com/lao-tseu-is-alive/Talunor/internal/sandbox
```

Demande à la suite ce qu'elle a réellement exécuté :

```bash
go test ./internal/sandbox/ -v -run Namespaces 2>&1 | grep -E '^(---|\s+---)'
```

Sur la plupart des machines, aujourd'hui :

```
--- SKIP: TestNamespacesEcho (0.00s)
--- SKIP: TestNamespacesNoNetwork (0.00s)
--- SKIP: TestNamespacesRootReadOnly (0.00s)
--- SKIP: TestNamespacesTimeout (0.00s)
```

Tous les tests qui exercent le vrai backend sautaient, parce qu'Ubuntu réapplique

```bash
cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns   # 1 = le backend ne peut pas tourner
```

au fil des mises à jour (la leçon 10 et le piège 14 d'`AGENTS.md` préviennent au sujet de
ce sysctl ; personne ne prévenait que son retour *ferait taire les tests*). Le saut est un
comportement correct — un contributeur sans espaces de noms utilisateur ne devrait pas
avoir une suite rouge. L'échec, c'est qu'`ok` et `ok` se ressemblent, que quatre tests
aient tourné ou aucun.

> **L'idée centrale.** `t.Skip` convertit « je ne peux pas vérifier ceci » en « rien à
> signaler ». C'est un défaut raisonnable pour la portabilité et un défaut désastreux
> pour la confiance. L'environnement dont tes tests ont besoin fait partie du *contrat*
> de ta suite, et rien ne l'impose par défaut.

### La réponse de conception : rendre la décision testable sans le privilège

Tu ne peux pas faire apparaître des espaces de noms utilisateur sur chaque portable. Tu
peux, en revanche, empêcher la décision intéressante d'en dépendre. C'est pourquoi le
correctif n'ajoute pas simplement un `if` dans `childMain` — une fonction qui ne peut que
`exec` ou `os.Exit`, donc qu'aucun test ne peut appeler. Il extrait le jugement dans une
fonction ordinaire :

```go
func verifyChildIdentity(pid int, tokenFD *os.File, envValue string) error
```

`pid` est un paramètre, pas `os.Getpid()`. Le fd est un paramètre, pas le fd 3. La valeur
d'environnement est un paramètre, pas `os.Getenv`. Toute la décision devient un test
tabulaire sur de simples `os.Pipe()`, plus un test en sous-processus qui relance le
binaire de test avec le déclencheur ambiant — **ni l'un ni l'autre n'exigent root, ni
espaces de noms, ni conteneur** :

```bash
go test ./internal/sandbox/ -v -run 'VerifyChild|ChildTrigger'
```

Neuf cas, aucun sauté, sur n'importe quelle machine Linux et n'importe quel runner de CI.
La partie qui exige encore des privilèges (montages, `pivot_root`, rlimits) reste où elle
était ; la partie qui décide *s'il faut en faire quoi que ce soit* n'en dépend plus.

Note la petite précaution du test en sous-processus : il lance l'enfant avec
`-test.run=^$`. Si la garde régresse un jour, l'enfant détourné n'exécute *aucun* test au
lieu de rentrer à nouveau dans celui-ci et de se dupliquer sans fin. **Quand un test
lance ton propre binaire de test, pars du principe que ce que tu testes est cassé.**

## Pratique — laisse le sysctl décider de ton verdict

L'idée frappe quand le même code passe puis échoue sans autre changement qu'un réglage
noyau. Sur une machine Linux où tu peux utiliser `sudo` :

```bash
# 1. Regarde où tu en es.
cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns

# 2. Casse le correctif exprès : dans internal/sandbox/namespaces_linux.go, remplace
#    `defer tokenR.Close()` par un simple `tokenR.Close()` à la ligne avant `cmd.Run()`
#    — exactement le défaut de la partie 3.

# 3. Avec la restriction ACTIVE (sysctl = 1) :
go test ./internal/sandbox/          # => ok. La sandbox est morte et la suite est ravie.

# 4. Lève-la, et repose la même question :
scripts/allow-unprivileged-userns.sh
go test ./internal/sandbox/ -run Namespaces -v
#  --- FAIL: TestNamespacesEcho
#      echo output = "sandbox child: cannot read token from fd 3: read sandbox-token:
#      bad file descriptor\n\n[exit status 127]"

# 5. Remets le `defer`, vérifie le vert, et remets le sysctl si tu préfères :
#    scripts/allow-unprivileged-userns.sh --restore
```

L'étape 3 est celle qu'il faut laisser infuser. Rien dans ton code n'a changé entre
l'étape 3 et l'étape 4. Ce sont tes *preuves* qui ont changé.

## Les principes

```text
Un test sauté n'est pas un test réussi — et « ok » ne te dira jamais lequel des deux tu as.
```

1. **Vérifie le constat, puis dimensionne-le.** « Tente un `pivot_root` sur l'hôte » et
   « meurt au premier `mount` avec EPERM » méritent des priorités et des mots différents.
2. **Authentifie la forme avant la valeur.** Absent, vide ou de mauvaise longueur, c'est
   un rejet — pas une entrée pour `==`.
3. **Ne lis que ce qui t'appartient.** Un fd hérité est non typé et non fiable :
   `fstat`-le, borne la lecture, ne le `ReadAll` jamais dans `init()`.
4. **Sache quand tes fd sont dupliqués.** `exec.Cmd` copie `ExtraFiles` au `Start` ;
   extrémité d'écriture tôt, extrémité de lecture tard.
5. **Les balises de build gardent la compilation ; `t.Skip` garde l'exécution.** Elles ne
   sont pas interchangeables.
6. **Sors la décision du code privilégié.** Une fonction pure prenant pid, fd et secret en
   paramètres est testable partout ; un `childMain` qui ne fait qu'`exec` n'est testable
   nulle part.
7. **Audite tes sauts.** `go test ./... -v | grep SKIP` est une habitude de cinq secondes
   qui te dit ce que vaut vraiment ton vert.

## Checklist de fin

- [ ] J'ai reproduit le détournement par variable ambiante et je sais dire sa gravité réelle.
- [ ] J'ai fait l'expérience `/tmp/fdlab` et vu `bad file descriptor` devenir `TOKEN123` en
      déplaçant une ligne.
- [ ] Je peux expliquer pourquoi `"" == ""` mettait en échec la garde du jeton, et ce que
      `verifyChildIdentity` contrôle avant de comparer.
- [ ] Je sais dire ce qui déraille quand `io.ReadAll` rencontre un fd 3 qui ne lui appartient pas.
- [ ] J'ai lancé `GOOS=darwin go vet ./internal/sandbox/` et je comprends pourquoi un `t.Skip`
      n'a pas sauvé ce fichier de test.
- [ ] J'ai listé les sauts de ma propre suite et je sais lesquels cachent un vrai trou de
      capacité.
- [ ] J'ai fait la pratique du sysctl et vu un code identique passer, puis échouer.
- [ ] Je suis revenu sur `main` avec le `defer` restauré.

---

## 🎓 À propos de cette leçon

C'est le troisième post-mortem du cours (après les leçons 11 et 14) et le premier portant
sur un **correctif** plutôt que sur une fonctionnalité. Le patch disséqué ici a été généré
par une machine, c'est ainsi que l'épisode a commencé, mais ce cadrage n'est délibérément
pas la morale : un humain soigneux écrivant le même code aurait buté sur le défaut 1 à
l'identique, et la suite verte lui aurait menti tout aussi bien. La leçon 15 t'a appris à
vérifier ce qu'une revue *affirme* ; celle-ci pose la question plus difficile — **que
couvrent réellement tes preuves ?** La réponse de Talunor, en un commit : mesurer le
défaut avant de le corriger, extraire la décision du code privilégié, et ne jamais
confondre `ok` avec une case cochée.

Retour à l'[index du cours](../README.fr.md).
