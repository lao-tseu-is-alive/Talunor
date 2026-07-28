package memory

import (
	"context"
	"fmt"
)

// Layer 21 — supersession, and the TRUST MODEL that governs it.
//
// A memory can be *corrected*: a newer fact retires an older, incompatible one.
// The dangerous question is WHO is allowed to correct WHOM. There is no universal
// answer — it is a policy that encodes what this particular agent's memory is FOR,
// and it must be a deliberate decision, not an inherited "the user is always right".
//
// The two failure modes this policy has to thread (worked examples in ADR 0003):
//
//   - A user says "the earth is flat." The user is authoritative about *themselves*,
//     NOT about the world, so this must never overwrite a world fact. (It is stored,
//     if at all, as a BELIEF about the user — a different subject — and the arbiter
//     classifies it UNRELATED, so it never reaches this policy.)
//   - A Verified intrusion-detection tool observes "signature X is mitigated by Y."
//     The tool IS authoritative about what it observed, so this SHOULD be able to
//     retire a stale, model-inferred belief about that signature.
//
// A single global rank ("user > tool > model") satisfies neither. What both share:
// authority is *per-domain*, and the source's provenance is a PROXY for its authority.
// Supersedes below is the whole trust model in one place — the ~10 lines you decide.
// A different agent (security, ops, research) replaces THIS function and nothing else.

// supersedeAuthority ranks a source's authority for the purpose of retiring a belief,
// under the DEFAULT (personal-assistant) trust model:
//
//	2  user_stated / tool_observed — authoritative: the user about themselves, a
//	   Verified tool about what it observed. Either may retire a belief in its domain.
//	1  unspecified — legacy/unknown: may retire only the non-authoritative model.
//	0  model_inferred — never authoritative enough to retire a belief on the strength
//	   of the model's own guess (the humility rule, extended from Layer 17's confidence
//	   guard to truth itself).
func supersedeAuthority(p Provenance) int {
	switch p {
	case ProvenanceUserStated, ProvenanceToolObserved:
		return 2
	case ProvenanceUnspecified:
		return 1
	default: // ProvenanceModelInferred
		return 0
	}
}

// Supersedes is the memory's TRUST MODEL: may a NEW fact from provenance `newer`
// retire an existing, incompatible fact from provenance `older`? It answers only the
// *authority* question — the arbiter has already decided the two facts are the same
// subject and incompatible (see internal/agent). A model inference never supersedes;
// otherwise a source may retire beliefs of its own authority level or lower.
//
// THIS is the function to change for a different agent. Its being one small, named,
// documented place — rather than scattered comparisons — is the point: a trust model
// you can read, test, and consciously own. (Layer 21; see ADR 0003.)
func Supersedes(newer, older Provenance) bool {
	na := supersedeAuthority(newer)
	if na == 0 {
		return false // the model's own inference retires nothing.
	}
	return na >= supersedeAuthority(older)
}

// Supersede retires oldID in favour of newID: it soft-marks the old fact
// (superseded_by = newID) so recall skips it while the row survives for audit and
// reversal. Idempotent-ish: re-pointing an already-superseded fact just updates the
// pointer. It does NOT verify the trust policy — the caller (agent.learnFrom) applies
// Supersedes first.
func (s *Store) Supersede(ctx context.Context, oldID, newID int64) error {
	if oldID == newID {
		return fmt.Errorf("supersede: a fact cannot supersede itself (#%d)", oldID)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE memories SET superseded_by = ? WHERE id = ?`, newID, oldID)
	if err != nil {
		return fmt.Errorf("supersede %d by %d: %w", oldID, newID, err)
	}
	return nil
}
