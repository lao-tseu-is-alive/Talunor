// This file is everything the agent does AFTER a turn has answered: reflection.
// It distils durable facts from the turn's sources, decides whether each one is new,
// a restatement, or a contradiction, and records the evidence — all on a background
// worker so none of it sits on the turn's critical path (Layer 18).
//
// Split out of agent.go in v0.22.5 — same package, same code, no new abstractions.
// The reading order the course uses (Lesson 20) is preserved: reflect, learnFrom,
// learnOneFact, then the helpers that decide what is worth learning at all.

package agent

import (
	"context"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// reflectObservation is one tool result gathered during a turn, a candidate source
// of durable facts (Layer 20). verified is true when the tool declared its output a
// deterministic, structured fact (tools.Verified) — which decides whether a fact
// distilled from it is tool_observed or (the honest default) model_inferred.
type reflectObservation struct {
	tool     string
	result   string
	verified bool
}

// reflectJob is one unit of deferred learning: the sources of a completed turn.
// The user message is always present; observations/assistant are optional. Turn
// ids anchor the evidence trail (Layer 20).
type reflectJob struct {
	userInput       string
	userTurnID      int64
	assistantAnswer string
	assistantTurnID int64
	observations    []reflectObservation
}

// reflectQueueCap bounds the reflection backlog. A human converses far slower
// than reflection completes, so it rarely fills; if it does, Turn blocks briefly
// (backpressure) rather than dropping learning or spawning unbounded goroutines.
const reflectQueueCap = 8

// reflect is the agent's learning step: it distils durable facts from the turn's
// SOURCES and stores each as semantic memory (memory.KindFact). Layer 20 widens it
// from "the user's message" to what the agent also observed and (opt-in) said:
//
//   - the user message      → user_stated  (always)
//   - each tool observation → tool_observed if the tool is tools.Verified, else
//     model_inferred (an LLM interpreting a tool's text is inference, not
//     observation — honest by default). Trivial/empty observations are skipped.
//   - the assistant answer  → model_inferred, only when Config.ReflectAssistant
//     (off by default: learning from one's own output is the echo-chamber risk).
//
// The SYSTEM assigns each fact's provenance from which source produced it — the
// model is never asked to label its own provenance (Layer 16's honesty rule), which
// is why sources are extracted separately, not in one combined call. It is
// best-effort (a failure never disturbs the already-streamed reply), consolidates a
// restatement onto the existing row (Layer 17), and records an evidence row per
// store/reinforce (Layer 20). Runs on the background worker (Layer 18) → its
// decisions go to the debug trace, not a closed transcript.
func (a *Agent) reflect(ctx context.Context, job reflectJob) {
	if a.extractor == nil {
		return
	}
	// Each source gets its own question, and the question fixes the ATTRIBUTION:
	// provenance (who said it, Layer 16/20) and subject (what it is about, Layer
	// 23). Both are assigned here, by the system, from the source — never read back
	// out of what the model wrote. See ADRs 0002 and 0004.
	//
	// User message: the primary, highest-provenance source, asked about the user.
	a.learnFrom(ctx, job.userInput, memory.UserSaid(), job.userTurnID)
	// Tool observations: asked about the WORLD, which is what a tool observes.
	// Verified tools yield tool_observed; everything else is model_inferred (the
	// honest default — an LLM reading a tool's text is still inference).
	for _, o := range job.observations {
		if !worthReflecting(o) {
			continue
		}
		a.learnFrom(ctx, truncateForReflect(o.result), memory.Observed(o.verified), job.userTurnID)
	}
	// Assistant answer: opt-in, model_inferred about the world (EvidenceCredibility
	// 0 → never raises confidence on its own; the echo-chamber guard from Layer 17).
	if a.cfg.ReflectAssistant && job.assistantAnswer != "" {
		a.learnFrom(ctx, job.assistantAnswer, memory.Observed(false), job.assistantTurnID)
	}
}

// learnFrom extracts durable facts from one source's text and stores or
// consolidates each, tagging it with the source's provenance/confidence and
// appending an evidence row (turnID, source) for the audit trail. All bookkeeping
// is best-effort. confidence is scaled by the model's calibration; the
// consolidation gain also folds in the source's credibility as INDEPENDENT evidence
// (user/tool raise confidence; the model echoing itself does not — Layer 17).
func (a *Agent) learnFrom(ctx context.Context, text string, attr memory.Attribution, turnID int64) {
	if strings.TrimSpace(text) == "" {
		return
	}
	// The subject selects the QUESTION asked of this text, and the answer is then
	// stamped with that same subject — the model is never asked what its output is
	// about (Layer 23).
	facts, err := a.extractor.Extract(ctx, text, attr.Subject)
	if err != nil {
		a.trace("reflect.error", "source", attr.String(), "err", err)
		return
	}
	prov := attr.Provenance
	conf := clamp01(memory.BaseConfidence(prov) * a.cfg.ModelConfidence)
	gain := clamp01(consolidationGainBase * memory.EvidenceCredibility(prov) * a.cfg.ModelConfidence)
	for _, f := range facts {
		a.learnOneFact(ctx, f, attr, conf, gain, turnID)
	}
	if len(facts) > 0 {
		a.trace("reflect", "source", attr.String(), "extracted", len(facts), "confidence", conf, "gain", gain)
	}
}

// learnOneFact stores, consolidates, or supersedes a single distilled fact. With the
// arbiter off (DisableArbiter) it is the Layer 20 path: nearest near-identical fact →
// consolidate, else store. With the arbiter on (Layer 21) it looks a bit WIDER for a
// contradiction candidate and asks the arbiter how the two relate:
//
//	RESTATES   → consolidate onto the existing row (Layer 17).
//	UNRELATED  → store as a new, distinct fact (fixes over-consolidation of merely
//	             embedding-near-but-different facts).
//	SUPERSEDES → the trust model (memory.Supersedes) decides: if this source is
//	             authoritative enough to retire the old belief, store the new fact and
//	             soft-supersede the old; otherwise DROP the new fact — the authoritative
//	             old belief stands (e.g. the model must not overwrite what the user said).
//
// LAYER 23: the candidate search is scoped to the fact's SUBJECT first. A claim about
// the user and a claim about the world are not rivals — they coexist — so a
// cross-subject neighbour is not a candidate at all: it never reaches the arbiter (one
// model call saved) and cannot be retired by arithmetic that never runs. That check is
// deterministic, which is the point: before it, the belief-vs-world separation existed
// only in the arbiter's judgement and in the extraction prompt's wording.
func (a *Agent) learnOneFact(ctx context.Context, f string, attr memory.Attribution, conf, gain float64, turnID int64) {
	_, arbiterOff := a.arbiter.(noArbiter)

	radius := a.cfg.SupersedeMaxDistance
	if arbiterOff {
		radius = a.cfg.DedupMaxDistance
	}
	cand, ok := a.knownFact(ctx, f, radius, attr.Subject)
	if !ok {
		a.storeNewFact(ctx, f, attr, conf, turnID)
		return
	}

	rel := RelRestates
	if !arbiterOff {
		r, err := a.arbiter.Classify(ctx, f, cand.Content)
		if err != nil {
			a.trace("arbiter.error", "err", err) // safe default: RelRestates (never retires a memory).
		} else {
			rel = r
		}
	}

	candAttr := memory.Attr(cand.Provenance, cand.Subject)
	switch rel {
	case RelSupersedes:
		if memory.Supersedes(attr, candAttr) {
			m, err := a.store.RememberFact(ctx, f, attr, conf)
			if err != nil {
				return
			}
			a.recordEvidence(ctx, m.ID, turnID, attr.Provenance)
			if err := a.store.Supersede(ctx, cand.ID, m.ID); err != nil {
				a.trace("supersede.error", "old", cand.ID, "new", m.ID, "err", err)
				return
			}
			a.trace("supersede", "old", cand.ID, "oldAttr", candAttr.String(),
				"new", m.ID, "newAttr", attr.String())
		} else {
			// The trust model forbids it — the old belief is more authoritative than
			// this source. The refusal stands, but LAYER 24 stops it from also being
			// an act of forgetting: the disagreement is recorded as counter-evidence
			// against the incumbent, which is what makes that fact report Contested.
			//
			// The refused claim is deliberately NOT stored as a memory. A stored fact
			// is a recallable fact, and recall is exactly the authority just denied it;
			// it lives only as the detail of this row. Nor does it move the incumbent's
			// confidence — letting a source that lost the explicit authority argument
			// win a partial one by arithmetic is the back door ADR 0005 closes.
			a.trace("supersede.denied", "newAttr", attr.String(), "oldAttr", candAttr.String(),
				"old", cand.ID)
			if err := a.store.RecordCounterEvidence(ctx, cand.ID, turnID, attr.Provenance, f); err != nil {
				a.trace("counterevidence.error", "fact", cand.ID, "err", err)
			}
		}
	case RelUnrelated:
		a.storeNewFact(ctx, f, attr, conf, turnID)
	default: // RelRestates
		if err := a.store.ReinforceFact(ctx, cand.ID, gain); err == nil {
			a.recordEvidence(ctx, cand.ID, turnID, attr.Provenance)
		}
	}
}

// storeNewFact stores a fresh fact and records its first evidence row (best-effort).
func (a *Agent) storeNewFact(ctx context.Context, f string, attr memory.Attribution, conf float64, turnID int64) {
	if m, err := a.store.RememberFact(ctx, f, attr, conf); err == nil {
		a.recordEvidence(ctx, m.ID, turnID, attr.Provenance)
	}
}

// recordEvidence appends one support row for a fact (best-effort; a failure is
// traced, never fatal). See memory.Store.RecordEvidence.
func (a *Agent) recordEvidence(ctx context.Context, factID, turnID int64, prov memory.Provenance) {
	if err := a.store.RecordEvidence(ctx, factID, turnID, prov); err != nil {
		a.trace("evidence.error", "fact", factID, "err", err)
	}
}

// toolVerified reports whether the named tool declares its output a deterministic,
// structured fact (tools.Verified) — which routes a distilled fact to tool_observed
// rather than model_inferred. Unknown or non-verified tools are false.
func (a *Agent) toolVerified(name string) bool {
	if a.tools == nil {
		return false
	}
	t, ok := a.tools.Get(name)
	if !ok {
		return false
	}
	v, ok := t.(tools.Verified)
	return ok && v.Verified()
}

// trivialTools produce no durable facts (deterministic scratch values or a view of
// memory itself), so their observations are skipped before spending an extraction
// call on them. web_fetch/bash are NOT here — they can carry durable facts.
var trivialTools = map[string]bool{"calculator": true, "current_time": true, "recall_memory": true}

// reflectObservationMaxRunes caps how much of a tool observation is handed to the
// extractor, so a large body (a fetched page, long shell output) can't blow up the
// reflection call. The head usually carries any durable fact.
const reflectObservationMaxRunes = 2000

// worthReflecting drops observations that can't yield a durable fact: empty, an
// error observation, the "(no output)" sentinel, or a trivial tool's output.
func worthReflecting(o reflectObservation) bool {
	r := strings.TrimSpace(o.result)
	if r == "" || r == "(no output)" || strings.HasPrefix(r, "error:") {
		return false
	}
	return !trivialTools[o.tool]
}

// truncateForReflect caps text to reflectObservationMaxRunes runes.
func truncateForReflect(s string) string {
	if r := []rune(s); len(r) > reflectObservationMaxRunes {
		return string(r[:reflectObservationMaxRunes])
	}
	return s
}

// reflectWorker is the single goroutine that owns deferred learning. It processes
// reflection jobs in order until the channel is closed (by Close), draining any
// still queued. One worker means store writes from reflection are serialised with
// each other — and database/sql serialises them against a turn's own reads on the
// pinned single connection.
func (a *Agent) reflectWorker() {
	defer a.workerWG.Done()
	for {
		select {
		case job := <-a.reflectCh:
			a.reflect(a.bgCtx, job)
			a.reflectWG.Done()
		case <-a.closing:
			// Shutdown announced: finish what is already queued, then exit. This is
			// the drain Close waits on — `default` is what ends it, so the worker
			// never blocks waiting for work that can no longer arrive.
			for {
				select {
				case job := <-a.reflectCh:
					a.reflect(a.bgCtx, job)
					a.reflectWG.Done()
				default:
					return
				}
			}
		}
	}
}

// enqueueReflect hands a completed turn's sources to the background worker and
// returns at once, so reflection stays off the turn's critical path. If the queue
// is full it blocks briefly (backpressure) rather than dropping the learning. With
// no worker (should not happen after New) it falls back to reflecting inline.
//
// The send waits on shutdown as well as on room in the queue. A turn goroutine can
// still be in flight when Close() runs (the TUI quits on a key, without cancelling
// the signal context), and a bare send that blocks on a full queue would then panic
// when Close closes the channel. Learning is best-effort by design, so on shutdown
// the job is dropped — never at the cost of a panic on the way out.
func (a *Agent) enqueueReflect(job reflectJob) {
	if a.reflectCh == nil {
		a.reflect(context.Background(), job)
		return
	}
	// Held for the whole send: Close cannot declare the queue shut while a sender
	// is still inside it.
	a.closeMu.RLock()
	defer a.closeMu.RUnlock()
	if a.closed {
		return // shutting down; learning is best-effort, so drop it.
	}

	a.reflectWG.Add(1)
	select {
	case a.reflectCh <- job:
	case <-a.closing:
		a.reflectWG.Done() // dropped: the worker is on its way out.
	}
}

// consolidationGainBase is the base fraction of the way to the confidence ceiling
// that one restatement of a fact earns (before credibility and calibration
// weighting). Small, so trust grows gradually with repeated corroboration.
const consolidationGainBase = 0.34

// knownFact returns the nearest already-stored fact within maxDist of the given one
// — the candidate reflect consolidates a restatement onto, or (Layer 21) asks the
// arbiter about. Only existing KindFact rows count: a raw conversation turn that
// happens to sit nearby must not block the first distillation of that turn. maxDist
// is the caller's radius (tight DedupMaxDistance for consolidation, wider
// SupersedeMaxDistance for contradictions).
func (a *Agent) knownFact(ctx context.Context, fact string, maxDist float64, about memory.Subject) (memory.Hit, bool) {
	// Use the consolidation-aware recall so a restatement of a long-neglected
	// (soft-forgotten) fact still finds the old row and reinforces it, instead of
	// silently inserting a near-duplicate the plain Recall would leave orphaned
	// below the forget floor forever.
	hits, err := a.store.RecallForConsolidation(ctx, fact, 3, maxDist)
	if err != nil {
		return memory.Hit{}, false
	}
	for _, h := range hits {
		// Same subject or nothing (Layer 23): "User believes the earth is flat" and
		// "The earth is round" embed close together, but they are claims about
		// different things — neither consolidates onto nor retires the other.
		if h.Kind == memory.KindFact && memory.SameSubject(about, h.Subject) {
			return h, true
		}
	}
	return memory.Hit{}, false
}

// reinforceRecalled strengthens the memories that shaped this turn: being recalled
// and injected into the prompt is a signal they matter, so bump their salience and
// refresh their decay clock (Layer 17). Best-effort — a bookkeeping failure must
// not disturb the reply.
func (a *Agent) reinforceRecalled(ctx context.Context, hits []memory.Hit) {
	if len(hits) == 0 {
		return
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	if err := a.store.Reinforce(ctx, ids); err != nil {
		a.trace("reinforce.error", "err", err)
	}
}
