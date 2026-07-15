//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Environment variables used to hand a run's configuration from the parent
// process to the re-executed child (see childMain). They are consumed and
// scrubbed before the user's script runs, so they never leak into its env.
const (
	envChild    = "TALUNOR_SANDBOX_CHILD"
	envScript   = "TALUNOR_SANDBOX_SCRIPT"
	envRootfs   = "TALUNOR_SANDBOX_ROOTFS_DIR"
	envFSBytes  = "TALUNOR_SANDBOX_FSBYTES"
	envMemBytes = "TALUNOR_SANDBOX_MEMBYTES"
	envCPUSecs  = "TALUNOR_SANDBOX_CPUSECS"
)

// namespaces is the from-scratch backend. It re-executes Talunor's own binary
// as an unprivileged, rootless "container" init (see the package doc and Run for
// the isolation it sets up). It is deliberately educational: strong enough to
// contain honest mistakes and casual probing, but NOT a security boundary
// against a determined adversary — there is no seccomp filter, so the full Linux
// syscall surface is reachable. Prefer the nerdctl/docker backend for real
// untrusted code.
type namespaces struct {
	rootfs string // prepared read-only busybox rootfs
}

// init hijacks the process when it is the re-executed sandbox child: instead of
// returning to main() it becomes the container init, sets up the namespaces'
// interior, and execs the script. This is the standard "/proc/self/exe re-exec"
// trick (à la Docker's libcontainer): the child shares our binary but branches
// here before any normal startup happens.
func init() {
	if os.Getenv(envChild) == "1" {
		childMain() // never returns
	}
}

// newNamespaces validates that unprivileged user namespaces are usable and that
// a busybox is available, then prepares (once, cached) the read-only rootfs.
func newNamespaces() (Sandbox, error) {
	if err := userNSAvailable(); err != nil {
		return nil, err
	}
	rootfs, err := prepareRootfs()
	if err != nil {
		return nil, err
	}
	return &namespaces{rootfs: rootfs}, nil
}

func (*namespaces) Name() string { return "namespaces" }

// Run launches the child in fresh user/mount/pid/uts/net/ipc namespaces. The
// USER namespace makes us rootless (child-root maps to the invoking uid); the
// NET namespace is empty, so there is no network unless lim.Network is set (not
// yet supported here — an empty netns is the whole point). The child sets up the
// rootfs and execs the script; see childMain.
func (n *namespaces) Run(ctx context.Context, script string, lim Limits) (string, error) {
	if strings.TrimSpace(script) == "" {
		return "", errors.New("empty script")
	}
	if lim.Network {
		return "", errors.New("namespaces backend does not support networking; use TALUNOR_SANDBOX=nerdctl")
	}
	timeout := lim.Timeout
	if timeout <= 0 {
		timeout = DefaultLimits().Timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "/proc/self/exe")
	cmd.Env = append(os.Environ(),
		envChild+"=1",
		envScript+"="+script,
		envRootfs+"="+n.rootfs,
		envFSBytes+"="+strconv.FormatInt(lim.FSBytes, 10),
		envMemBytes+"="+strconv.FormatInt(lim.MemBytes, 10),
		envCPUSecs+"="+strconv.Itoa(int(timeout.Seconds())+1),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUTS | syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
		Unshareflags: syscall.CLONE_NEWNS, // don't leak our mounts back to the host
		// Map child-root (0) to the invoking user: rootless. setgroups=deny is
		// written for us because GidMappingsEnableSetgroups defaults to false,
		// which is what makes the gid map writable unprivileged.
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		Pdeathsig:   syscall.SIGKILL, // if we die, take the sandbox with us
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return truncate(buf.String()) + fmt.Sprintf("\n[timed out after %s]", timeout), nil
	}
	if ctx.Err() != nil {
		return truncate(buf.String()), ctx.Err()
	}
	out := buf.String()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return truncate(out) + exitNote(exitErr.ExitCode()), nil
	}
	if err != nil {
		return truncate(out), fmt.Errorf("sandbox: namespaces run failed: %w", err)
	}
	return truncate(out), nil
}

// userNSAvailable reports a clear error when unprivileged user namespaces are
// disabled (some distros gate them behind sysctls).
func userNSAvailable() error {
	// Debian/Ubuntu: kernel.unprivileged_userns_clone must be 1.
	if b, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil {
		if strings.TrimSpace(string(b)) == "0" {
			return errors.New("unprivileged user namespaces are disabled " +
				"(kernel.unprivileged_userns_clone=0); enable them or use TALUNOR_SANDBOX=nerdctl")
		}
	}
	// Any kernel: a zero cap here means userns is off.
	if b, err := os.ReadFile("/proc/sys/user/max_user_namespaces"); err == nil {
		if strings.TrimSpace(string(b)) == "0" {
			return errors.New("user namespaces are disabled (user.max_user_namespaces=0); " +
				"use TALUNOR_SANDBOX=nerdctl")
		}
	}
	// Ubuntu 24.04+ gates unprivileged userns behind AppArmor: an unconfined
	// binary can create a userns but cannot write its uid_map, so rootless
	// mapping fails with EPERM. Detect it and explain the fix.
	if b, err := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns"); err == nil {
		if strings.TrimSpace(string(b)) == "1" {
			return errors.New("unprivileged user namespaces are AppArmor-restricted " +
				"(kernel.apparmor_restrict_unprivileged_userns=1, the Ubuntu 24.04+ default); " +
				"run `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0` to allow them, " +
				"or use TALUNOR_SANDBOX=nerdctl")
		}
	}
	return nil
}

// childMain is the container init: it runs inside the new namespaces, builds the
// isolated root, clamps resources, drops privileges, and execs the script. It
// never returns — it either execs the shell or exits with a diagnostic.
func childMain() {
	script := os.Getenv(envScript)
	rootfs := os.Getenv(envRootfs)
	fsBytes, _ := strconv.ParseInt(os.Getenv(envFSBytes), 10, 64)
	memBytes, _ := strconv.ParseInt(os.Getenv(envMemBytes), 10, 64)
	cpuSecs, _ := strconv.Atoi(os.Getenv(envCPUSecs))

	die := func(err error) {
		fmt.Fprintln(os.Stderr, "sandbox child: "+err.Error())
		os.Exit(127)
	}

	_ = unix.Sethostname([]byte("talunor-sandbox"))

	if err := setupRoot(rootfs, fsBytes); err != nil {
		die(err)
	}
	if err := clampResources(memBytes, cpuSecs); err != nil {
		die(err)
	}
	if err := dropPrivileges(); err != nil {
		die(err)
	}

	// A clean, minimal environment for the script — none of our TALUNOR_* vars.
	env := []string{
		"PATH=/bin:/usr/bin:/sbin:/usr/sbin",
		"HOME=/tmp",
		"TMPDIR=/tmp",
		"PS1=# ",
		"TERM=dumb",
	}
	// Exec replaces this process, so the shell becomes pid 1 of the namespace;
	// when it exits the kernel tears the whole sandbox down.
	if err := unix.Exec("/bin/sh", []string{"sh", "-c", script}, env); err != nil {
		die(fmt.Errorf("exec /bin/sh: %w", err))
	}
}

// setupRoot pivots into a fresh, read-only view of the rootfs with a private
// mount namespace, a size-capped writable /tmp, a minimal /dev, and its own
// /proc. After this returns, "/" is the busybox rootfs and nothing on the host
// filesystem is reachable.
func setupRoot(rootfs string, fsBytes int64) error {
	// Make every mount we inherited private so nothing propagates to the host.
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make / private: %w", err)
	}
	// pivot_root requires the new root to be a mount point: bind it onto itself.
	if err := unix.Mount(rootfs, rootfs, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("bind rootfs: %w", err)
	}
	if err := unix.Chdir(rootfs); err != nil {
		return fmt.Errorf("chdir rootfs: %w", err)
	}
	const oldRoot = ".old_root"
	_ = os.Mkdir(oldRoot, 0o700) // rootfs is still writable (bind is rw here)
	if err := unix.PivotRoot(".", oldRoot); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// Fresh /proc for the new pid namespace.
	if err := unix.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}
	// Size-capped writable scratch space; nosuid/nodev/noexec-relaxed (exec on so
	// scripts can write and run a helper if they must).
	tmpOpts := fmt.Sprintf("size=%d,mode=1777", fsBytes)
	if err := unix.Mount("tmpfs", "/tmp", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, tmpOpts); err != nil {
		return fmt.Errorf("mount /tmp: %w", err)
	}
	// Minimal /dev: bind the few device nodes commands actually expect, sourced
	// from the old root before we detach it.
	if err := unix.Mount("tmpfs", "/dev", "tmpfs", unix.MS_NOSUID, "mode=0755"); err != nil {
		return fmt.Errorf("mount /dev: %w", err)
	}
	for _, dev := range []string{"null", "zero", "full", "random", "urandom", "tty"} {
		target := "/dev/" + dev
		src := "/" + oldRoot + "/dev/" + dev
		if f, err := os.OpenFile(target, os.O_CREATE, 0o666); err == nil {
			f.Close()
		}
		// Best effort: if the host lacks one, skip it rather than fail the run.
		_ = unix.Mount(src, target, "", unix.MS_BIND, "")
	}

	// Detach the old root, then re-remount our rootfs read-only (non-recursive so
	// /proc, /tmp and /dev keep their own flags).
	if err := unix.Unmount("/"+oldRoot, unix.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}
	if err := unix.Mount("", "/", "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
		return fmt.Errorf("remount / read-only: %w", err)
	}
	return nil
}

// clampResources applies per-process rlimits. Memory (RLIMIT_AS) and CPU time
// (RLIMIT_CPU) are the useful ones; a file-size cap (RLIMIT_FSIZE) backs up the
// tmpfs size limit. Note: there is no reliable rootless pids cap here (cgroup
// delegation is usually unavailable and RLIMIT_NPROC is per-host-uid, which
// would throttle the user's own processes) — the memory cap plus the hard
// timeout are what actually contain a fork bomb.
func clampResources(memBytes int64, cpuSecs int) error {
	if memBytes > 0 {
		lim := &unix.Rlimit{Cur: uint64(memBytes), Max: uint64(memBytes)}
		if err := unix.Setrlimit(unix.RLIMIT_AS, lim); err != nil {
			return fmt.Errorf("rlimit AS: %w", err)
		}
	}
	if cpuSecs > 0 {
		lim := &unix.Rlimit{Cur: uint64(cpuSecs), Max: uint64(cpuSecs)}
		if err := unix.Setrlimit(unix.RLIMIT_CPU, lim); err != nil {
			return fmt.Errorf("rlimit CPU: %w", err)
		}
	}
	// Cap any single file at the writable-fs budget (belt to the tmpfs braces).
	fsize := &unix.Rlimit{Cur: 64 << 20, Max: 64 << 20}
	_ = unix.Setrlimit(unix.RLIMIT_FSIZE, fsize)
	nofile := &unix.Rlimit{Cur: 256, Max: 256}
	_ = unix.Setrlimit(unix.RLIMIT_NOFILE, nofile)
	return nil
}

// dropPrivileges forbids privilege escalation and empties the capability set, so
// the script cannot regain host powers even via a setuid binary. Within the user
// namespace these caps never mapped to real host privileges anyway; this is
// defense in depth.
func dropPrivileges() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	// Drop every capability from the bounding set (best effort per-cap).
	for cap := 0; cap <= unix.CAP_LAST_CAP; cap++ {
		_ = unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(cap), 0, 0, 0)
	}
	// Clear effective/permitted/inheritable sets.
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3, Pid: 0}
	var data [2]unix.CapUserData // all zero = no capabilities
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capset: %w", err)
	}
	return nil
}
