// Package agent implements Talunor's cognitive loop. A single Turn ties the
// three substrates together:
//
//	Perceive : take the user's input.
//	Recall   : fetch relevant long-term memories (KNN, thresholded) and the
//	           recent short-term turns.
//	Reason   : build a prompt (system + memories + recent turns + input) and
//	           stream a completion from the LLM provider.
//	Store    : remember the user turn and the assistant turn (short-term ring
//	           + long-term store) so the next turn can recall them.
//
// This is the first layer that remembers across turns.
package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/policy"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// Plan-approval modes for Config.ApprovalMode.
const (
	// ApprovalPlan asks once for the whole plan, then runs its (in-plan) tools
	// without further prompts — the human's plan approval is the consent.
	ApprovalPlan = "plan"
	// ApprovalStep asks for the whole plan AND still confirms each risky step.
	ApprovalStep = "step"
	// ApprovalHighRisk skips the whole-plan prompt: the plan is advisory and the
	// per-call policy gate prompts as usual (≈ the pre-planner behaviour, plus a
	// visible plan).
	ApprovalHighRisk = "highrisk"
)

// Planner-failure modes for Config.PlannerFallback. When planning fails (the model
// cannot produce a valid plan.Plan, even after the planner's retry), the turn must
// still do *something* — and the choice is a safety decision, not an implementation
// detail: the plan is what caps which tools the executor may offer, so falling back
// to the plain ReAct loop silently returns the very freedom the user asked to
// remove by enabling the planner.
const (
	// FallbackFailClosed answers WITHOUT tools (the default). The turn still
	// responds — a plan failure is not a reason to say nothing — but it cannot act,
	// because no approved plan bounds what acting would mean.
	FallbackFailClosed = "fail_closed"
	// FallbackAsk asks the human whether to proceed with no plan. Approving grants
	// the plain ReAct loop for this turn; declining behaves as FallbackFailClosed.
	// The point is that the *change of execution contract* is what gets consented to.
	FallbackAsk = "ask"
	// FallbackReact runs the plain ReAct loop (the pre-v0.22.2 behaviour): every
	// tool offered, policy and per-call approval still applied. Explicit opt-in, so
	// the loss of the plan's cap is a choice someone made rather than a default.
	FallbackReact = "react"
)

// execCtx carries the per-turn constraints the planner imposes on the ReAct loop.
// Its zero value is the pre-planner behaviour: every tool offered, the policy's own
// per-step approval applied.
type execCtx struct {
	// allowTools, when non-nil, is the only set of tools offered to the model this
	// turn — the structural "cap" that keeps a planned execution on-plan (the model
	// literally cannot call a tool the approved plan didn't name). Nil = all tools.
	allowTools map[string]bool
	// reapproveAtOrAbove sets how much a whole-plan approval can cover. A step the
	// policy wants approved still re-prompts — with its *live* arguments — when its
	// RiskLevel is at or above this level. This closes the gap that a blanket
	// plan-approval binds the tool *name* but not the arguments the ReAct executor
	// ultimately chooses. RiskLow (the zero value) means "always re-prompt when the
	// policy asks" — the pre-planner behaviour. The policy's deny always holds.
	reapproveAtOrAbove plan.RiskLevel
	// approvedArgs binds the whole-plan approval to the ARGUMENTS it displayed, not
	// just to the tool names. It maps a tool to the canonical forms of the argument
	// payloads the approved plan showed for it; non-nil only under a plan approval.
	//
	// Why it exists: the approval prompt renders the plan's concrete arguments and
	// calls it the set of actions being consented to, but execution was capped by
	// tool NAME alone. `web_fetch` under its allowlist gate is RiskMedium, and plan
	// mode only re-prompted at RiskHigh — so a plan displaying a fetch to host A
	// could execute a fetch to host B under the earlier approval. The destination,
	// the part that actually matters, was never bound.
	//
	// A call whose arguments are NOT in this set is DRIFT: the human consented to
	// something else, so it re-prompts with the live arguments whatever its risk
	// level. A step the plan left argument-less binds nothing, so any call to it
	// counts as drift — the human approved a tool, not an action.
	approvedArgs map[string][]string
}

// argsDrifted reports whether a live call departs from what the approved plan
// displayed. False when no plan approval is in force (nothing to drift from).
// Arguments are compared CANONICALLY — re-marshalled from the parsed JSON — so key
// order and whitespace do not read as a deviation.
func (e execCtx) argsDrifted(tool string, args json.RawMessage) bool {
	if e.approvedArgs == nil {
		return false
	}
	live := canonicalJSON(args)
	for _, approved := range e.approvedArgs[tool] {
		if approved == live {
			return false
		}
	}
	return true
}

// canonicalJSON normalises an argument payload for comparison. Unparseable input
// falls back to its trimmed text: a payload we cannot read is compared as-is rather
// than silently treated as equal to anything.
func canonicalJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return strings.TrimSpace(string(raw))
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(b)
}

// Config tunes an Agent.
type Config struct {
	// SystemPrompt frames every conversation.
	SystemPrompt string
	// RecallK is the maximum number of long-term memories to retrieve per turn.
	RecallK int
	// RecallMaxDistance drops memories whose cosine distance exceeds it, so only
	// relevant ones are injected. 0 is a meaningful value — it keeps all k
	// matches (no thresholding) — so, unlike the other numeric fields, New does
	// *not* substitute DefaultConfig's value for a zero here. Set it explicitly
	// (DefaultConfig uses 0.75) to enable thresholding.
	RecallMaxDistance float64
	// ShortTermCap is the number of recent turns kept verbatim as immediate
	// context.
	ShortTermCap int
	// Options is passed through to the provider on every call.
	Options llm.Options

	// Extractor is the reflection step: after each turn it distils durable facts
	// from the user's message into semantic memory (memory.KindFact). If nil, New
	// installs a default LLM-based extractor over the agent's own provider; inject
	// DisableReflection() to turn reflection off.
	Extractor FactExtractor
	// DedupMaxDistance suppresses storing a freshly-extracted fact when an
	// existing fact lies within this cosine distance, so restating something does
	// not pile up near-duplicate facts. Small = "only skip near-identical facts".
	DedupMaxDistance float64
	// ReflectAssistant, when true, also distils facts from the assistant's own
	// answer (tagged model_inferred). Off by default: learning from the model's own
	// output is the echo-chamber risk, and EvidenceCredibility(model_inferred)=0
	// already stops it raising confidence — so it is opt-in at the Config level and
	// deliberately not wired to an env var (Layer 20; keeps a single TALUNOR_REFLECT).
	ReflectAssistant bool

	// Arbiter classifies how a freshly-learned fact relates to a near-neighbour it
	// already holds — restates / supersedes / unrelated (Layer 21). If nil, New
	// installs a default LLM arbiter over the agent's provider; inject
	// DisableArbiter() to turn Layer 21 off (fall back to Layer 20 consolidation).
	Arbiter FactArbiter
	// SupersedeMaxDistance is the cosine radius within which the arbiter looks for a
	// contradiction candidate. It is DELIBERATELY WIDER than DedupMaxDistance: a
	// contradiction is "same topic, different value" (near, but not near-identical),
	// so the tight dedup radius would miss it. 0 → a sensible default (0.35).
	SupersedeMaxDistance float64

	// ModelConfidence scales the confidence of every fact the agent *learns* (via
	// reflection), in [0,1]. It is the calibration link: set it from a `calibrate`
	// run's overall pass-rate so a fact learned from an unreliable model does not
	// silently gain the authority of an established one. 0 (unset) → 1.0 (no scaling).
	// cmd/talunor wires it from TALUNOR_MODEL_CONFIDENCE.
	ModelConfidence float64
	// RecallMinConfidence drops recalled long-term memories whose confidence is below
	// it (0 = off). A guardrail against feeding low-confidence "facts" back into the
	// prompt as if established. cmd/talunor wires it from TALUNOR_RECALL_MIN_CONFIDENCE.
	RecallMinConfidence float64

	// Tools, when set, are offered to the model each turn; the agent runs an
	// act→observe loop, executing any tool calls and feeding results back until
	// the model answers. Nil = a plain conversational turn (no tools).
	Tools *tools.Registry
	// MaxToolIters caps the act/observe rounds per turn, so a confused model can't
	// loop forever. Defaults to 6.
	MaxToolIters int

	// Policy decides, before each tool call, whether it may run automatically,
	// needs human approval, or is denied outright (see internal/policy). If nil,
	// New installs the default policy.ToolGatePolicy backed by Tools — which
	// preserves the pre-policy behaviour (each tool's own Approvable/ApprovableFor
	// gate) — or an AllowAllPolicy when there are no tools to gate.
	Policy policy.Policy

	// Planner, when set, makes the agent plan before it acts: each turn it produces
	// an explicit, inspectable plan.Plan, the human approves it (see ApprovalMode),
	// and the ReAct loop then executes *capped to the plan's tools*. Nil (the
	// default) keeps the plain emergent ReAct loop — tools are discovered one call
	// at a time. cmd/talunor wires it from TALUNOR_PLANNER.
	Planner Planner
	// ApprovalMode governs how a plan is approved, one of ApprovalPlan (approve the
	// whole plan once, then run its tools without per-step prompts), ApprovalStep
	// (approve the plan, and still confirm each risky step), or ApprovalHighRisk
	// (no whole-plan prompt; the plan is advisory and per-call policy prompts as
	// usual). Empty defaults to ApprovalPlan. Ignored when Planner is nil.
	ApprovalMode string
	// PlannerFallback governs what happens when planning FAILS, one of
	// FallbackFailClosed (default: answer without tools), FallbackAsk (ask the human
	// whether to proceed unplanned) or FallbackReact (the plain ReAct loop, every
	// tool offered). Empty defaults to FallbackFailClosed. Ignored when Planner is
	// nil. cmd/talunor wires it from TALUNOR_PLANNER_FALLBACK.
	PlannerFallback string

	// Debug, when non-nil, receives a structured trace of the loop's otherwise
	// invisible decisions: which memories were recalled (id + distance), which
	// tools ran, and what reflection stored or skipped. It is a teaching/debug
	// aid, off by default; cmd/talunor wires it from TALUNOR_DEBUG. The trace may
	// include snippets of recalled memory content, so it is opt-in and local.
	Debug *slog.Logger
}

// DefaultConfig returns sensible defaults for a conversational agent.
func DefaultConfig() Config {
	return Config{
		SystemPrompt: "You are Talunor, a helpful assistant with long-term memory. " +
			"When the provided memories are relevant, use them to answer; " +
			"otherwise ignore them and answer normally. Do not mention the memory system unless asked.",
		RecallK:              8,
		RecallMaxDistance:    0.75,
		ShortTermCap:         6,    // ~3 exchanges.
		DedupMaxDistance:     0.20, // near-identical facts only (consolidation).
		SupersedeMaxDistance: 0.35, // wider: "same topic, different value" (contradiction).
		MaxToolIters:         6,
	}
}

// Agent owns the memory substrates and the LLM provider and runs the loop.
type Agent struct {
	store     *memory.Store
	short     *memory.ShortTerm
	provider  llm.Provider
	extractor FactExtractor
	arbiter   FactArbiter
	tools     *tools.Registry
	policy    policy.Policy
	planner   Planner
	// lastPlan is the most recent plan produced this session, surfaced by the
	// /plan command. It is written from the turn goroutine (runPlanned) and read
	// from the UI goroutine (/plan), so it is atomic: even though the front-ends
	// normally drain a turn before reading, nothing in the type guaranteed the
	// happens-before, and a future concurrent front-end would inherit the race.
	lastPlan atomic.Pointer[plan.Plan]
	cfg      Config
	// screenDebug, when true, streams the loop's otherwise-invisible decisions
	// (recall rankings, reflection results) inline as dimmed notes, so the user
	// can watch them in the transcript. Toggled at runtime via SetScreenDebug (the
	// /debug command); distinct from Config.Debug, which logs to a file/stderr.
	// Atomic: written from the UI goroutine (/debug), read from the turn goroutine.
	screenDebug atomic.Bool

	// Async reflection (Layer 18): the learning step (a second LLM call) runs on a
	// single background worker so it is off the turn's critical path — the reply
	// streams and the turn ends immediately, learning catches up behind it. Turn
	// enqueues a job on reflectCh; one worker processes them in order. reflectWG
	// counts outstanding jobs (Quiesce waits on it); workerWG tracks the worker
	// goroutine (Close waits on it after draining). bgCtx scopes background
	// reflection so it outlives the turn's context but is cancelled on Close.
	reflectCh chan reflectJob
	reflectWG sync.WaitGroup
	workerWG  sync.WaitGroup
	bgCtx     context.Context
	bgCancel  context.CancelFunc
	// closing is closed by Close to announce shutdown. reflectCh itself is NEVER
	// closed: a turn goroutine can still be enqueuing while Close runs (the TUI
	// quits on a key without cancelling the turn's context), and a send parked on
	// a full queue panics if the channel is closed underneath it. Announcing
	// shutdown on a SECOND channel removes that hazard by construction — both the
	// sender and the worker select on it, and nothing ever sends on a closed one.
	//
	// closeMu/closed then make the handoff exact rather than merely safe. Closing
	// the channel unparks a blocked sender, but the queue is BUFFERED, so a send
	// racing shutdown could still land a job in the buffer after the worker had
	// gone — a job nobody would ever run, and whose reflectWG slot would never be
	// released (Quiesce would hang forever). Close takes the write lock to wait
	// until no sender is inside enqueueReflect, then sets closed; from that point
	// every enqueue drops its job immediately.
	closing   chan struct{}
	closeMu   sync.RWMutex
	closed    bool
	closeOnce sync.Once
	// drainTimeout bounds Close's wait for queued reflection (defaults to
	// closeDrainTimeout; overridable in tests to keep them fast).
	drainTimeout time.Duration
}

// New builds an Agent. Zero-valued config fields fall back to DefaultConfig,
// with one deliberate exception: RecallMaxDistance is left as-is because 0 is a
// meaningful value there (keep all k matches — see its field doc). Callers that
// want thresholding must set it (DefaultConfig uses 0.75); cmd/talunor does.
func New(store *memory.Store, provider llm.Provider, cfg Config) *Agent {
	def := DefaultConfig()
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = def.SystemPrompt
	}
	if cfg.RecallK <= 0 {
		cfg.RecallK = def.RecallK
	}
	if cfg.ShortTermCap <= 0 {
		cfg.ShortTermCap = def.ShortTermCap
	}
	if cfg.DedupMaxDistance <= 0 {
		cfg.DedupMaxDistance = def.DedupMaxDistance
	}
	if cfg.SupersedeMaxDistance <= 0 {
		cfg.SupersedeMaxDistance = def.SupersedeMaxDistance
	}
	// ModelConfidence defaults to 1.0 (no scaling); both knobs are clamped to [0,1].
	if cfg.ModelConfidence <= 0 {
		cfg.ModelConfidence = 1.0
	}
	cfg.ModelConfidence = clamp01(cfg.ModelConfidence)
	cfg.RecallMinConfidence = clamp01(cfg.RecallMinConfidence)
	if cfg.MaxToolIters <= 0 {
		cfg.MaxToolIters = def.MaxToolIters
	}
	// Default reflection: the agent uses its own LLM provider to write its
	// semantic memory. Callers disable it with DisableReflection().
	if cfg.Extractor == nil {
		cfg.Extractor = newLLMExtractor(provider, cfg.Options)
	}
	// Default arbiter (Layer 21): classify a new fact vs. a near-neighbour so
	// contradictions supersede instead of piling up. DisableArbiter() falls back to
	// Layer 20 consolidation.
	if cfg.Arbiter == nil {
		cfg.Arbiter = newLLMArbiter(provider, cfg.Options)
	}
	// Default guardrail: consult each tool's own approval interfaces, exactly
	// reproducing pre-policy behaviour. With no tools there is nothing to gate,
	// so an AllowAllPolicy avoids handing the tool-gate a nil lookup.
	if cfg.Policy == nil {
		if cfg.Tools != nil {
			cfg.Policy = policy.NewToolGate(cfg.Tools.Get)
		} else {
			cfg.Policy = policy.AllowAllPolicy{}
		}
	}
	// ApprovalMode only matters when planning; default it and reject typos so an
	// unknown value never silently weakens the gate.
	switch cfg.ApprovalMode {
	case ApprovalPlan, ApprovalStep, ApprovalHighRisk:
	default:
		cfg.ApprovalMode = ApprovalPlan
	}
	// Same rule for the planner-failure mode: an unknown value must resolve to the
	// SAFEST option, never to the most permissive one. A typo in a config that
	// governs "what happens when the safety mechanism breaks" is exactly when you
	// want the conservative default.
	switch cfg.PlannerFallback {
	case FallbackFailClosed, FallbackAsk, FallbackReact:
	default:
		cfg.PlannerFallback = FallbackFailClosed
	}
	a := &Agent{
		store:        store,
		short:        memory.NewShortTerm(cfg.ShortTermCap),
		provider:     provider,
		extractor:    cfg.Extractor,
		arbiter:      cfg.Arbiter,
		tools:        cfg.Tools,
		policy:       cfg.Policy,
		planner:      cfg.Planner,
		cfg:          cfg,
		drainTimeout: closeDrainTimeout,
	}
	// Start the async reflection worker (Layer 18). It owns the deferred learning
	// step; callers should Close the agent to drain it on shutdown.
	a.reflectCh = make(chan reflectJob, reflectQueueCap)
	a.closing = make(chan struct{})
	a.bgCtx, a.bgCancel = context.WithCancel(context.Background())
	a.workerWG.Add(1)
	go a.reflectWorker()
	return a
}

// Quiesce blocks until every enqueued reflection job has been processed (or ctx is
// cancelled). It does not stop the worker — more turns may follow. Tests use it to
// wait for async learning before inspecting the store; it is also a clean way to
// checkpoint "all caught up".
func (a *Agent) Quiesce(ctx context.Context) error {
	done := make(chan struct{})
	go func() { a.reflectWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeDrainTimeout bounds how long Close waits for queued reflection to finish
// before it cancels the background context to force the worker out. A human
// converses far slower than reflection completes, so the queue is normally short
// and drains well within this; the timeout only matters when a reflection call
// is stuck on an unresponsive provider.
const closeDrainTimeout = 5 * time.Second

// Close shuts the agent down cleanly: it stops accepting new reflection jobs and
// drains those already queued (so learning in flight is not lost), then releases
// the background context. It is idempotent. Call it before closing the store —
// the worker writes to the store while draining. No Turn may run after Close.
//
// The drain is BOUNDED: bgCtx has no deadline of its own, so a reflection call
// stuck on an unresponsive provider would otherwise make workerWG.Wait() (and
// thus the whole process) hang forever. Close waits up to closeDrainTimeout, then
// cancels bgCtx to unblock the in-flight LLM call so the worker can exit — the
// still-in-flight learning is best-effort and is dropped rather than wedging
// shutdown. This is why the cancel can precede the final wait: "cancel, then
// wait" is the correct shutdown order once a drain deadline exists.
func (a *Agent) Close() error {
	a.closeOnce.Do(func() {
		if a.closing != nil {
			close(a.closing) // unparks any sender waiting on a full queue…
			a.closeMu.Lock() // …then wait for every sender to leave enqueueReflect…
			a.closed = true  // …and refuse the ones that arrive from here on.
			a.closeMu.Unlock()
		}
		done := make(chan struct{})
		go func() { a.workerWG.Wait(); close(done) }()
		select {
		case <-done: // drained within the deadline — nothing lost.
		case <-time.After(a.drainTimeout):
			if a.bgCancel != nil {
				a.bgCancel() // unblock a stuck in-flight reflection…
			}
			<-done // …then finish waiting for the worker to return.
		}
		if a.bgCancel != nil {
			a.bgCancel() // idempotent; releases bgCtx in the fast path too.
		}
	})
	return nil
}

// trace emits a structured debug event when Config.Debug is set; it is a no-op
// otherwise, so instrumentation call sites stay unconditional and cheap.
func (a *Agent) trace(msg string, args ...any) {
	if a.cfg.Debug != nil {
		a.cfg.Debug.Debug(msg, args...)
	}
}

// clamp01 constrains x to [0,1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
