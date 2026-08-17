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
// authority is *per-domain*, and the source's provenance is a PROXY for its authority
// IN THAT DOMAIN. Supersedes below is the whole trust model in one place — the ~15
// lines you decide. A different agent (security, ops, research) replaces THIS function
// and nothing else.
//
// LAYER 23 closed the gap between that paragraph and the code. Until then this
// function saw only provenance, so "per-domain" was enforced nowhere: it relied on the
// reflection prompt attributing user facts ("User …") and on the arbiter calling a
// belief and a world fact UNRELATED. Two model calls, no deterministic backstop — and
// when both failed, Supersedes(user_stated, tool_observed) was true, so an
// unattributed "the earth is flat" retired a Verified tool's observation. The domain is
// now DATA (see subject.go), and the gate reads it. ADR 0004.

// supersedeAuthority ranks a source's authority for the purpose of retiring a belief
// ABOUT A GIVEN SUBJECT, under the DEFAULT (personal-assistant) trust model:
//
//	2  user_stated about the USER — you are the authority on yourself.
//	0  user_stated about the WORLD — you are not. Saying it does not make it so, and
//	   memory must not let conviction retire an observation. (Your world-claims are
//	   still remembered — as facts about you; see SubjectUser.)
//	2  tool_observed — a Verified tool is authoritative about what it observed,
//	   whichever subject it observed it about.
//	1  unspecified — legacy/unknown: may retire only the non-authoritative model.
//	0  model_inferred — never authoritative enough to retire a belief on the strength
//	   of the model's own guess (the humility rule, extended from Layer 17's confidence
//	   guard to truth itself).
//
// Note the user_stated/world row is unreachable from today's sources — reflection only
// ever asks the user's message for facts about the user. It is stated anyway, because
// a trust model is read as a POLICY: the matrix must say what it would do, so that
// whoever adds a source (a "correct my knowledge base" mode, an imported document)
// reads the answer instead of discovering it.
func supersedeAuthority(a Attribution) int {
	switch a.Provenance {
	case ProvenanceUserStated:
		if a.Subject.normalize() == SubjectWorld {
			return 0
		}
		return 2
	case ProvenanceToolObserved:
		return 2
	case ProvenanceUnspecified:
		return 1
	default: // ProvenanceModelInferred
		return 0
	}
}

// Supersedes is the memory's TRUST MODEL: may a NEW fact with attribution `newer`
// retire an existing, incompatible fact attributed `older`?
//
// It answers the *authority* question in two steps, and the first is the one Layer 23
// added:
//
//  1. DIFFERENT SUBJECTS NEVER SUPERSEDE. A claim about the user and a claim about the
//     world cannot contradict each other — they coexist. This is the flat-earth carve-out
//     of ADR 0003, moved out of the arbiter's judgement and into arithmetic: it now holds
//     even when the extractor drops the attribution AND the arbiter wrongly answers
//     SUPERSEDES, which is exactly the case that was reachable before.
//  2. Within one subject, authority decides: a model inference never supersedes;
//     otherwise a source may retire beliefs of its own authority level or lower.
//
// THIS is the function to change for a different agent. Its being one small, named,
// documented place — rather than scattered comparisons — is the point: a trust model
// you can read, test, and consciously own. (Layers 21 & 23; ADRs 0003 and 0004.)
func Supersedes(newer, older Attribution) bool {
	if !SameSubject(newer.Subject, older.Subject) {
		return false // different domains: not a contradiction, so nothing to retire.
	}
	na := supersedeAuthority(newer)
	if na == 0 {
		return false // the model's own inference — and your world-claims — retire nothing.
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
