//go:build !linux

package sandbox

import "errors"

// newNamespaces is unavailable off Linux: the backend is built on Linux user,
// mount, pid, and network namespaces. Use the nerdctl/docker backend instead.
func newNamespaces() (Sandbox, error) {
	return nil, errors.New("the namespaces sandbox backend requires Linux; use TALUNOR_SANDBOX=nerdctl")
}
