package memory

// The sqlite-vector / sqlite-ai extensions are dlopen'd at runtime by SQLite
// and reference libm symbols (fmaxf, exp, log, …) which vector.so does NOT link
// itself — it expects them to be resolvable in the global symbol scope.
//
// Marking libm as a NEEDED dependency of the Go binary is not sufficient: those
// symbols do not end up in the scope SQLite's loader searches, and the load
// fails with an (unhelpfully empty) "undefined symbol" error. The reliable fix
// is to explicitly dlopen libm with RTLD_GLOBAL at startup, which publishes its
// symbols into the global scope that subsequent extension loads resolve against.

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stddef.h>

static int talunorPreloadLibm(void) {
	// RTLD_GLOBAL makes libm's symbols available to later dlopen'd libraries.
	return dlopen("libm.so.6", RTLD_NOW | RTLD_GLOBAL) == NULL ? 1 : 0;
}
*/
import "C"

import "fmt"

func init() {
	if C.talunorPreloadLibm() != 0 {
		// Non-fatal: extension loading will surface a clearer error if this
		// ever matters, but a missing system libm is worth flagging loudly.
		fmt.Println("warning: could not preload libm.so.6 into global scope")
	}
}
