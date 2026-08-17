package memory

// Layer 23 — WHAT a fact is about, as data.
//
// Layer 16 recorded a fact's provenance: WHO it came from. Layer 21 turned that
// into an authority ranking (Supersedes) so memory could correct itself. ADR 0003
// argued — correctly — that "authority is per-domain": a user is authoritative
// about themselves, a Verified tool about what it observed, and neither about
// everything. But the *code* only ever saw provenance, so "per-domain" lived in
// the reflection prompt ("write each fact starting with User") and in an LLM
// arbiter's judgement that a belief and a world fact are different subjects.
//
// Two model calls therefore stood between a user's world-claim and the retirement
// of a world fact, and when both failed the deterministic gate had nothing to say:
// Supersedes(user_stated, tool_observed) was true, so "the earth is flat" could
// retire a Verified tool's observation. See ADR 0004.
//
// Subject makes the missing half explicit. It is assigned by the SYSTEM, the same
// way provenance is (ADR 0002) and for the same reason: not by asking the model to
// label its own output, but by asking a QUESTION whose answer can only be of one
// kind, and stamping the answer with the question that was asked. Reflection asks
// the user's message "what durable facts about the USER are here?" and a tool
// observation "what durable facts about the WORLD does this state?" — so the
// subject is known before the model replies, and a model that ignores the framing
// cannot launder a world-claim into user authority.

// Subject is what a fact is ABOUT — the domain in which its source may or may not
// carry authority. It is deliberately coarse: two values plus a legacy one. A
// richer taxonomy would need the agent to classify truth by topic, which ADR 0003
// rejected (an agent that decides what a claim is *really* about is the
// over-confident thing this design avoids).
type Subject string

const (
	// SubjectUser: a claim about the user — their identity, preferences, history,
	// goals, and their BELIEFS about anything, including the world. "User believes
	// the earth is flat" is a user-subject fact: the claim it records is a fact
	// about the user, not about the earth.
	SubjectUser Subject = "user"
	// SubjectWorld: a claim about anything outside the user — an observed system,
	// a domain fact, the content of a document or a tool's output.
	SubjectWorld Subject = "world"
	// SubjectUnspecified: unknown subject. Every row written before Layer 23 has
	// this, and migration 6 does NOT guess a subject for them — inventing an
	// attribution for stored data would be exactly the laundering this layer
	// exists to prevent. Such rows keep the pre-Layer-23 (weaker) guarantee: see
	// SameSubject.
	SubjectUnspecified Subject = "unspecified"
)

// Valid reports whether s is a known subject.
func (s Subject) Valid() bool {
	switch s {
	case SubjectUser, SubjectWorld, SubjectUnspecified:
		return true
	default:
		return false
	}
}

// normalize maps the empty/unknown subject to Unspecified, so a caller that
// forgets to set one gets the conservative legacy behaviour rather than a
// silently invalid column value.
func (s Subject) normalize() Subject {
	if !s.Valid() || s == "" {
		return SubjectUnspecified
	}
	return s
}

// Attribution is a fact's full credential: who stated it AND what it is about.
// Authority is a property of the pair, never of either half alone — that is the
// whole content of Layer 23, and the reason Supersedes takes this type rather
// than two Provenances.
type Attribution struct {
	Provenance Provenance
	Subject    Subject
}

// Attr builds an Attribution. Use it at the point where the SOURCE is known —
// the one place entitled to decide either field.
func Attr(p Provenance, s Subject) Attribution {
	return Attribution{Provenance: p, Subject: s.normalize()}
}

// UserSaid is the attribution of a fact distilled from the user's own message
// under the user-facts question: the user speaking about themselves.
func UserSaid() Attribution { return Attr(ProvenanceUserStated, SubjectUser) }

// Observed is the attribution of a fact distilled from a tool observation under
// the world-facts question. verified selects the Layer-20 tier: a tools.Verified
// tool yields tool_observed (authoritative in its domain), anything else stays
// model_inferred — an LLM reading a tool's text is still inference.
func Observed(verified bool) Attribution {
	if verified {
		return Attr(ProvenanceToolObserved, SubjectWorld)
	}
	return Attr(ProvenanceModelInferred, SubjectWorld)
}

// String renders an attribution for traces and /why.
func (a Attribution) String() string {
	return string(a.Provenance) + "/" + string(a.Subject.normalize())
}

// SameSubject reports whether two facts are in the same domain, and therefore
// whether one is even a candidate to contradict the other.
//
// An Unspecified subject is compatible with everything. That is a deliberate
// choice about legacy data, not an oversight: rows written before Layer 23 have
// no subject, and treating them as incomparable would freeze them permanently
// (nothing could ever correct them), while guessing their subject would be a
// fabrication. They keep exactly the pre-Layer-23 behaviour — the provenance gate
// alone — and new facts get the stronger one.
func SameSubject(a, b Subject) bool {
	a, b = a.normalize(), b.normalize()
	if a == SubjectUnspecified || b == SubjectUnspecified {
		return true
	}
	return a == b
}
