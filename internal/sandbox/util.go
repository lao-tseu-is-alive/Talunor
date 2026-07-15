package sandbox

import "strings"

// maxOutput caps the observation fed back to the model. A runaway command can
// print megabytes; the model neither needs nor should pay for all of it.
const maxOutput = 16 << 10 // 16 KiB

// truncate clips s to maxOutput, appending a marker when it had to cut.
func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n[output truncated]"
}

// shellQuote wraps s in single quotes for safe inclusion in a `sh -c` argument,
// escaping any embedded single quotes. This is belt-and-suspenders: the script
// is already passed as a distinct argv element, but the runtime backend nests it
// inside a container-side `sh -c`, so it must survive one shell parse.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
