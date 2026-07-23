package calibration

import "math"

// Stats summarises a set of numeric samples. It is where a standard deviation is
// actually meaningful: latency (and any future numeric verifier) is continuous, so
// mean ± stddev describes it. For a binary pass/fail outcome the variance is fixed
// by the pass-rate (a Bernoulli), so consistency there is read off the pass-rate's
// distance from 0 or 1 (a rate near 0.5 is "flaky") rather than a separate stddev.
type Stats struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"` // population standard deviation
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func newStats(xs []float64) Stats {
	if len(xs) == 0 {
		return Stats{}
	}
	min, max, sum := xs[0], xs[0], 0.0
	for _, x := range xs {
		sum += x
		if x < min {
			min = x
		}
		if x > max {
			max = x
		}
	}
	mean := sum / float64(len(xs))
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return Stats{
		N:      len(xs),
		Mean:   mean,
		Stddev: math.Sqrt(ss / float64(len(xs))),
		Min:    min,
		Max:    max,
	}
}
