package nodemgr

import (
	"math"
	"sync/atomic"
)

type ScoreWeights struct {
	Latency float64
	Speed   float64
	Health  float64
	Load    float64
}

func DefaultWeights() ScoreWeights {
	return ScoreWeights{
		Latency: 0.35,
		Speed:   0.25,
		Health:  0.25,
		Load:    0.15,
	}
}

type Scorer struct{ weights ScoreWeights }

func NewScorer(w ScoreWeights) *Scorer { return &Scorer{weights: w} }

func (s *Scorer) Score(n *Node) int32 {
	latencyMs := atomic.LoadInt64(&n.LatencyMs)
	inFlight := atomic.LoadInt32(&n.InFlight)

	latScore := 0.5
	if latencyMs > 0 {
		latScore = 1.0 - math.Min(float64(latencyMs)/1000.0, 1.0)
	}
	speedScore := 0.0
	healthScore := 0.0
	if n.Healthy {
		healthScore = 1.0
	}
	loadScore := 1.0 - math.Min(float64(inFlight)/10.0, 1.0)

	total := s.weights.Latency*latScore +
		s.weights.Speed*speedScore +
		s.weights.Health*healthScore +
		s.weights.Load*loadScore

	score := int32(total * 10000)
	atomic.StoreInt32(&n.Score, score)
	return score
}

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
				bestIdle, bestIdleScore = n, score
			}
		}
		if bestOverall == nil || score > bestOverallScore {
			bestOverall, bestOverallScore = n, score
		}
	}
	if bestIdle != nil {
		return bestIdle
	}
	return bestOverall
}
