package memory

import (
	"database/sql"
	"sort"
	"time"
)

// LAYER 22 — fusing the two arms of recall.
//
// The vector arm ranks by cosine distance (0 = identical, ~1 = unrelated); the
// lexical arm ranks by BM25 (a negative, unbounded, corpus-dependent number).
// These two numbers share no scale, no unit and no range, so any attempt to
// average or weight them directly is a fudge factor waiting to be tuned forever.
//
// Reciprocal Rank Fusion sidesteps the problem by throwing the scores away and
// keeping only each arm's ORDER:
//
//	rrf(memory) = Σ over arms  1 / (rrfK + rank_in_that_arm)
//
// A memory found by both arms accumulates from both, so corroboration wins
// without anyone choosing a weight. rrfK (60, the value from the original RRF
// paper) flattens the head of each list: it stops rank 1 from being worth
// dramatically more than rank 2, which is what you want when one arm is
// occasionally confidently wrong.
const (
	// rrfK is the rank-fusion damping constant.
	rrfK = 60.0
	// noVectorDistance marks a hit the vector arm never returned; a real distance
	// is in [0,2], so a negative sentinel cannot be mistaken for one. Use
	// Hit.HasVector rather than comparing against this.
	noVectorDistance = -1.0
)

// scanSource says which arm produced the row being scanned; the two queries
// return the same columns except for the final relevance value, which is a
// cosine distance for one and a BM25 rank for the other.
type scanSource int

const (
	scanVector scanSource = iota
	scanLexical
)

// scanHit reads one candidate row in the shared column order used by both arms.
// Keeping the projection in one place is what lets a lexical hit and a vector
// hit flow through exactly the same filtering, decay and scoring code.
func scanHit(rows *sql.Rows, src scanSource) (Hit, error) {
	var (
		h            Hit
		kind         string
		prov         string
		subject      string
		lastAccessed sql.NullString
		createdAt    string
		relevance    float64
	)
	if err := rows.Scan(&h.ID, &kind, &h.Role, &h.Content, &prov, &subject, &h.Confidence,
		&h.Salience, &lastAccessed, &h.AccessCount, &createdAt, &h.Contested, &relevance); err != nil {
		return Hit{}, err
	}
	h.Kind = Kind(kind)
	h.Provenance = Provenance(prov)
	h.Subject = Subject(subject).normalize()
	if ts, err := time.Parse(sqliteTimeLayout, createdAt); err == nil {
		h.CreatedAt = ts
	}
	if lastAccessed.Valid {
		if ts, err := time.Parse(sqliteTimeLayout, lastAccessed.String); err == nil {
			h.LastAccessed = ts
		}
	}
	switch src {
	case scanVector:
		h.Distance = relevance
	case scanLexical:
		h.Distance = noVectorDistance
		h.BM25 = relevance
	}
	return h, nil
}

// HasVector reports whether the vector arm retrieved this memory — i.e. whether
// Distance means anything for it.
func (h Hit) HasVector() bool { return h.Distance >= 0 }

// FromLexical reports whether the lexical arm retrieved this memory.
func (h Hit) FromLexical() bool { return h.LexicalRank > 0 }

// fuse merges the two candidate lists into one ranked result.
//
// Each arm arrives already ordered and already filtered (superseded rows in SQL;
// assistant turns, over-threshold distances and soft-forgotten memories by the
// caller), so fuse only decides ORDER — and it deliberately decides it two
// different ways:
//
//   - With one arm, the ranking must be exactly what that arm already produced.
//     A Talunor built without FTS5 has to behave like Layer 17 did, so the
//     classic score (1-distance)·confidence·effective-salience is kept verbatim.
//     This case is NOT redundant: **RRF is not the identity function on a single
//     list.** Ranking by 1/(60+rank)·conf·sal re-orders results compared with
//     (1-distance)·conf·sal, because the two relevance terms fall off
//     differently — a hit at distance 0.10 with confidence 0.5 scores 0.45
//     classically (first) but 0.0082 under RRF (second, behind a 0.70/1.0 hit at
//     rank 2). Falling through to the fused branch would therefore silently
//     re-order recall for every user who never asked for hybrid.
//   - With two arms, there is no shared scale to multiply, so RRF supplies the
//     relevance term instead: score = rrf·confidence·effective-salience.
//
// Confidence and effective salience keep their Layer 16/17 meaning in both
// cases: relevance says "this is about your question", the other two say "and
// this is how much you should trust it and how much it still matters". Only the
// relevance term changes shape. Score is therefore comparable WITHIN one recall,
// not across builds — which is why /debug prints the arm ranks next to it.
func fuse(vector, lexical []Hit, k int) []Hit {
	for i := range vector {
		vector[i].VectorRank = i + 1
	}
	for i := range lexical {
		lexical[i].LexicalRank = i + 1
	}

	if len(lexical) == 0 {
		for i := range vector {
			h := &vector[i]
			h.Score = (1 - h.Distance) * h.Confidence * h.effSalience
		}
		return topK(vector, k)
	}

	// Merge on memory id: a hit found by both arms keeps its vector distance and
	// collects both ranks.
	byID := make(map[int64]*Hit, len(vector)+len(lexical))
	order := make([]int64, 0, len(vector)+len(lexical))
	for i := range vector {
		h := vector[i]
		byID[h.ID] = &h
		order = append(order, h.ID)
	}
	for i := range lexical {
		l := lexical[i]
		if existing, ok := byID[l.ID]; ok {
			existing.LexicalRank = l.LexicalRank
			existing.BM25 = l.BM25
			continue
		}
		h := l
		byID[h.ID] = &h
		order = append(order, h.ID)
	}

	fused := make([]Hit, 0, len(order))
	for _, id := range order {
		h := *byID[id]
		var rrf float64
		if h.VectorRank > 0 {
			rrf += 1 / (rrfK + float64(h.VectorRank))
		}
		if h.LexicalRank > 0 {
			rrf += 1 / (rrfK + float64(h.LexicalRank))
		}
		h.Score = rrf * h.Confidence * h.effSalience
		fused = append(fused, h)
	}
	return topK(fused, k)
}

// topK sorts by score, highest first, and truncates. The sort is stable so that
// equal scores keep the order the arms produced.
func topK(hits []Hit, k int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	return hits
}
