//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// prepareRootfs returns a directory usable as the sandbox root: a minimal
// busybox userland plus the mount points childMain needs (/proc, /tmp, /dev).
//
// Precedence:
//  1. TALUNOR_SANDBOX_ROOTFS — an existing rootfs directory to use as-is (must
//     contain a working /bin/sh). Nothing is built.
//  2. A cached busybox rootfs under the user cache dir, built once from a static
//     busybox (TALUNOR_SANDBOX_BUSYBOX or the first "busybox" on PATH).
func prepareRootfs() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("TALUNOR_SANDBOX_ROOTFS")); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "bin", "sh")); err != nil {
			return "", fmt.Errorf("TALUNOR_SANDBOX_ROOTFS=%s has no bin/sh: %w", dir, err)
		}
		return dir, nil
	}

	busybox, err := findBusybox()
	if err != nil {
		return "", err
	}

	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = os.TempDir()
	}
	root := filepath.Join(cacheBase, "talunor", "sandbox-rootfs")
	// A marker file records that a previous build finished cleanly.
	marker := filepath.Join(root, ".ready")
	if _, err := os.Stat(marker); err == nil {
		return root, nil
	}

	// Build into a sibling temp dir, then rename into place so a crash mid-build
	// never leaves a half-populated rootfs that looks ready.
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(root), "sandbox-rootfs-*")
	if err != nil {
		return "", fmt.Errorf("create rootfs build dir: %w", err)
	}
	defer os.RemoveAll(tmp) // no-op after a successful rename

	if err := buildBusyboxRootfs(tmp, busybox); err != nil {
		return "", err
	}
	if f, err := os.Create(filepath.Join(tmp, ".ready")); err == nil {
		f.Close()
	}
	_ = os.RemoveAll(root)
	if err := os.Rename(tmp, root); err != nil {
		// A racing builder may have won; if the destination is now ready, use it.
		if _, statErr := os.Stat(marker); statErr == nil {
			return root, nil
		}
		return "", fmt.Errorf("install rootfs: %w", err)
	}
	return root, nil
}

// findBusybox locates a statically-linked busybox to populate the rootfs with.
func findBusybox() (string, error) {
	if p := strings.TrimSpace(os.Getenv("TALUNOR_SANDBOX_BUSYBOX")); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("TALUNOR_SANDBOX_BUSYBOX=%s: %w", p, err)
		}
		return p, nil
	}
	if p, err := exec.LookPath("busybox"); err == nil {
		return p, nil
	}
	return "", errors.New("no busybox found for the namespaces rootfs; install busybox " +
		"(a static build is ideal) or set TALUNOR_SANDBOX_BUSYBOX / TALUNOR_SANDBOX_ROOTFS, " +
		"or use TALUNOR_SANDBOX=nerdctl")
}

// buildBusyboxRootfs lays out a throwaway rootfs at dir: the busybox binary, a
// symlink per applet (so /bin/ls, /bin/grep, … resolve), and the empty mount
// points childMain populates at run time.
func buildBusyboxRootfs(dir, busybox string) error {
	for _, sub := range []string{"bin", "proc", "tmp", "dev", "etc", ".old_root"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	dst := filepath.Join(dir, "bin", "busybox")
	if err := copyFile(busybox, dst, 0o755); err != nil {
		return fmt.Errorf("copy busybox: %w", err)
	}

	// One symlink per applet busybox provides, so PATH lookups for `ls`, `grep`,
	// `sh`, etc. all land on the single binary.
	applets, err := listApplets(busybox)
	if err != nil {
		return err
	}
	for _, a := range applets {
		link := filepath.Join(dir, "bin", a)
		if a == "busybox" {
			continue
		}
		_ = os.Remove(link)
		if err := os.Symlink("busybox", link); err != nil {
			return fmt.Errorf("symlink %s: %w", a, err)
		}
	}
	// Guarantee a shell exists even if `busybox --list` was empty for some reason.
	sh := filepath.Join(dir, "bin", "sh")
	if _, err := os.Lstat(sh); err != nil {
		if err := os.Symlink("busybox", sh); err != nil {
			return fmt.Errorf("symlink sh: %w", err)
		}
	}
	// A trivial /etc so tools that read it don't error noisily.
	_ = os.WriteFile(filepath.Join(dir, "etc", "hostname"), []byte("talunor-sandbox\n"), 0o644)
	return nil
}

// listApplets returns the applet names a busybox build supports.
func listApplets(busybox string) ([]string, error) {
	out, err := exec.Command(busybox, "--list").Output()
	if err != nil {
		// Some minimal builds don't support --list; fall back to a core set.
		return []string{"sh", "ls", "cat", "echo", "grep", "sed", "awk", "head",
			"tail", "wc", "sort", "uniq", "cut", "tr", "find", "env", "printf",
			"true", "false", "sleep", "date", "pwd", "mkdir", "rm", "cp", "mv",
			"touch", "chmod", "test", "expr", "seq", "tee", "xargs", "timeout"}, nil
	}
	var applets []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if a := strings.TrimSpace(line); a != "" {
			applets = append(applets, a)
		}
	}
	return applets, nil
}

// copyFile copies src to dst with the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
