package v1alpha1

import (
	"k8s.io/klog/v2"
)

// MaxScore is the upper bound of the normalized score.
const MaxScore = 100

// MinScore is the lower bound of the normalized score.
const MinScore = -100

// scoreNormalizer holds the minimum and maximum values for normalization,
// provides a normalize library to generate scores for AddOnPlacementScore.
type scoreNormalizer struct {
	min float64
	max float64
}

// NewScoreNormalizer creates a new instance of scoreNormalizer with given min and max values.
func NewScoreNormalizer(min, max float64) *scoreNormalizer {
	return &scoreNormalizer{
		min: min,
		max: max,
	}
}

// Normalize normalizes a given value to the range -100 to 100 based on the min and max values.
func (s *scoreNormalizer) Normalize(value float64) (score int32, err error) {
	if value > s.max {
		// If the value exceeds the maximum, set score to MaxScore.
		score = MaxScore
		// If the value is less than or equal to the minimum, set score to MinScore.
	} else if value <= s.min {
		score = MinScore
	} else {
		// Otherwise, normalize the value to the range -100 to 100.
		score = (int32)((MaxScore-MinScore)*(value-s.min)/(s.max-s.min) + MinScore)
	}

	klog.V(2).Infof("value = %v, min = %v, max = %v, score = %v", value, s.min, s.max, score)
	return score, nil
}
