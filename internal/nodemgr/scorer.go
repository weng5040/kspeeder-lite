package nodemgr

import (
	"math"
	"sync/atomic"
)

// ScoreWeights defines the contribution of each dimension to the total score.
// All weights sum to 1.0.
type ScoreWeights struct {
	Latency float64 // response latency weight
	Speed   float64 // download bandwidth weight
	Health  float64 // reliability weight
	Success float64 // track record weight
	Load    float64 // current load weight
}

// DefaultWeights returns the recommended scoring weights.
// Rationale:
//   - Latency (0.25): response speed is the primary user-facing metric
//   - Speed (0.25): bandwidth determines download completion time
//   - Health (0.25): reliability prevents thrashing to broken nodes
//   - Success (0.15): historical success builds trust but shouldn't dominate
//   - Load (0.10): load balancing is important but secondary to performance
func DefaultWeights() ScoreWeights {
	return ScoreWeights{
		Latency: 0.25,
		Speed:   0.25,
		Health:  0.25,
		Success: 0.15,
		Load:    0.10,
	}
}

// Scorer computes multi-dimensional node scores.
type Scorer struct {
	weights ScoreWeights
}

// NewScorer creates a scorer with the given weights.
func NewScorer(w ScoreWeights) *Scorer {
	return &Scorer{weights: w}
}

// Score computes a node's score on a 0-10000 scale (stored as int32 for atomic).
// Higher = better. All dimensions are normalized to [0, 1].
func (s *Scorer) Score(n *Node) int32 {
	latencyMs := atomic.LoadInt64(&n.LatencyMs)
	speedKBps := atomic.LoadInt64(&n.SpeedKBps)
	failCount := atomic.LoadInt32(&n.FailCount)
	successCount := atomic.LoadInt64(&n.SuccessCount)
	inFlight := atomic.LoadInt32(&n.InFlight)

	// Latency: 0ms=1.0, 1000ms=0.0, >1000ms=0.0
	latScore := 0.0
	if latencyMs > 0 {
		latScore = 1.0 - math.Min(float64(latencyMs)/1000.0, 1.0)
	} else {
		latScore = 0.5 // unknown latency → neutral
	}

	// Speed: 0KB/s=0.0, 100MB/s=1.0, capped
	speedScore := math.Min(float64(speedKBps)/102400.0, 1.0)

	// Health: failCount 0=1.0, failCount 5=0.0
	healthScore := 0.0
	if n.Healthy {
		healthScore = 1.0 - math.Min(float64(failCount)/5.0, 1.0)
	}

	// Success: 0=0.0, 100=0.5, 10000=1.0 (logarithmic)
	successScore := 0.0
	if successCount > 0 {
		successScore = math.Min(math.Log10(float64(successCount))/4.0, 1.0)
	}

	// Load: inflight 0=1.0, inflight 10=0.0
	loadScore := 1.0 - math.Min(float64(inFlight)/10.0, 1.0)

	total := s.weights.Latency*latScore +
		s.weights.Speed*speedScore +
		s.weights.Health*healthScore +
		s.weights.Success*successScore +
		s.weights.Load*loadScore

	// Scale to 0-10000 for int32 atomic storage
	score := int32(total * 10000)
	atomic.StoreInt32(&n.Score, score)
	return score
}

// SelectBest returns the best node from the candidate list.
// Strategy:
//   1. Prefer idle nodes (InFlight == 0)
//   2. If multiple idle, pick highest score
//   3. If none idle, pick highest score among all (load already penalizes)
func (s *Scorer) SelectBest(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	var bestIdle, bestOverall *Node
	var bestIdleScore, bestOverallScore int32

	for _, n := range nodes {
		if !n.Enabled || !n.Healthy {
			continue
		}
		score := s.Score(n)

		if n.IsIdle() {
			if bestIdle == nil || score > bestIdleScore {
				bestIdle = n
				bestIdleScore = score
			}
		}
		if bestOverall == nil || score > bestOverallScore {
			bestOverall = n
			bestOverallScore = score
		}
	}

	// Prefer idle nodes
	if bestIdle != nil {
		return bestIdle
	}
	return bestOverall
}
