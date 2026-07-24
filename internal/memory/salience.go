package memory

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// Layer 17 — salience, decay, and reinforcement (the retention half of learning).
//
// Two forces act on every memory:
//
//   - Decay is LAZY: nothing is written just to make a memory fade. The stored
//     `salience` is its value as of `last_accessed` (or `created_at` if never
//     recalled); the *effective* salience at read time is that value multiplied by
//     an exponential decay of the elapsed age. A memory that fades below
//     ForgetFloor is dropped from recall (soft-forgotten — the row survives and can
//     be revived by a restatement). This suits the pinned single connection: reads
//     never turn into writes (see Store's SetMaxOpenConns(1)).
//
//   - Reinforcement is EXPLICIT and happens at well-defined moments, never as a
//     side effect of Recall: Reinforce (a recalled memory mattered this turn) bumps
//     salience only; ReinforceFact (reflect saw the same fact restated) bumps
//     salience AND, weighted by the restatement's credibility as *independent*
//     evidence, confidence — this is consolidation. Repetition strengthens memory.
//
// The independence rule is what keeps confidence honest (see Layer 16): counting a
// model re-inferring its own fact as confirmation would build a self-reinforcing
// echo chamber, so only independent sources (user/tool) raise confidence; the model
// echoing itself raises salience but not confidence (EvidenceCredibility).

const (
	// defaultSalienceHalfLife is how long an un-recalled memory takes to lose half
	// its effective salience. Recall reinforces, so only genuinely neglected
	// memories fade; ~30 days keeps a single session's memories fully salient.
	defaultSalienceHalfLife = 30 * 24 * time.Hour
	// defaultForgetFloor is the effective salience below which a memory is dropped
	// from recall (soft forgetting). Small, so only long-neglected memories fade
	// out; the row is never deleted and a restatement revives it.
	defaultForgetFloor = 0.05

	// salienceBump is added to a memory's salience each time it is reinforced.
	salienceBump = 0.5
	// salienceCap bounds salience so a frequently-recalled memory cannot grow
	// without limit and drown out everything else.
	salienceCap = 4.0

	// confidenceCeiling is the most confidence repetition alone can earn: below 1.0
	// on purpose — restating a claim, however often, never makes it certain (the
	// humility of Layer 17). Direct high-provenance stores can still sit above it.
	confidenceCeiling = 0.98
)

// resolvedHalfLife / resolvedForgetFloor apply the package defaults when the store
// was configured with a zero (so a manually-built Config still behaves sanely).
func (s *Store) resolvedHalfLife() time.Duration {
	if s.cfg.SalienceHalfLife > 0 {
		return s.cfg.SalienceHalfLife
	}
	return defaultSalienceHalfLife
}

func (s *Store) resolvedForgetFloor() float64 {
	if s.cfg.ForgetFloor > 0 {
		return s.cfg.ForgetFloor
	}
	return defaultForgetFloor
}

// effectiveSalience decays a stored salience over the age since it was last
// touched. ref is last_accessed, or created_at if never accessed. Half-life form:
// after one half-life the factor is 0.5, after two 0.25, and so on. A non-positive
// half-life or age means no decay.
func effectiveSalience(salience float64, ref, now time.Time, halfLife time.Duration) float64 {
	if halfLife <= 0 || ref.IsZero() {
		return salience
	}
	age := now.Sub(ref)
	if age <= 0 {
		return salience
	}
	return salience * math.Exp2(-float64(age)/float64(halfLife))
}

// EvidenceCredibility weights a restatement by how much it counts as INDEPENDENT
// evidence for a fact, gating confidence reinforcement (salience is bumped
// regardless). A user restating something, or a tool re-observing it, is genuine
// corroboration; the model re-inferring its own earlier claim is not — counting it
// would let the agent talk itself into false certainty. Hence model_inferred earns
// zero confidence gain.
func EvidenceCredibility(p Provenance) float64 {
	switch p {
	case ProvenanceUserStated, ProvenanceToolObserved:
		return 1.0
	case ProvenanceModelInferred:
		return 0.0
	default:
		return 0.5
	}
}

// reinforcedConfidence moves confidence a fraction gain of the way toward the
// ceiling: bounded, monotonic, with diminishing returns (each repetition helps
// less than the last). gain ≤ 0 leaves it unchanged.
func reinforcedConfidence(current, gain float64) float64 {
	if gain <= 0 {
		return current
	}
	if current >= confidenceCeiling {
		return current
	}
	return current + gain*(confidenceCeiling-current)
}

// Reinforce marks memories as freshly useful: it bumps their salience (capped),
// increments access_count, and resets the decay clock (last_accessed = now). Call
// it for the memories a turn actually recalled — recall strengthens memory. It does
// NOT touch confidence: being retrieved is not new evidence that a claim is true.
func (s *Store) Reinforce(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+2)
	args = append(args, salienceCap, salienceBump)
	for _, id := range ids {
		args = append(args, id)
	}
	q := fmt.Sprintf(`
		UPDATE memories
		   SET salience = min(?, salience + ?),
		       access_count = access_count + 1,
		       last_accessed = datetime('now')
		 WHERE id IN (%s)`, placeholders)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("reinforce salience: %w", err)
	}
	return nil
}

// ReinforceFact consolidates a restated fact: it bumps salience like Reinforce and,
// by gain, raises confidence toward the ceiling (diminishing returns). gain should
// already fold in the restatement's credibility as independent evidence and the
// model's calibration (see the caller); gain ≤ 0 bumps salience but leaves
// confidence untouched (e.g. the model echoing its own inference). This is what
// lets a fact stated three times live as ONE increasingly trusted row instead of
// three near-duplicates.
func (s *Store) ReinforceFact(ctx context.Context, id int64, gain float64) error {
	// Salience + access bookkeeping always apply — a restatement is a use.
	if err := s.Reinforce(ctx, []int64{id}); err != nil {
		return err
	}
	// Confidence rises only on independent evidence (gain>0), and only up to the
	// ceiling (the WHERE guard makes an already-certain fact a no-op).
	if gain > 0 {
		q := `UPDATE memories
		         SET confidence = confidence + ? * (? - confidence)
		       WHERE id = ? AND confidence < ?`
		if _, err := s.db.ExecContext(ctx, q, gain, confidenceCeiling, id, confidenceCeiling); err != nil {
			return fmt.Errorf("reinforce fact %d confidence: %w", id, err)
		}
	}
	return nil
}
