package channelrpm

import "testing"

func TestNoCapMeansFullWeight(t *testing.T) {
	Reset()
	for range 50 {
		RecordHit(1)
	}
	if f := HeadroomFactor(1); f != 1 {
		t.Fatalf("no cap should give factor 1, got %v", f)
	}
}

func TestLearnedCapShrinksWeight(t *testing.T) {
	Reset()
	for range 60 {
		RecordHit(2)
	}
	LearnCapFromRateLimit(2)
	f := HeadroomFactor(2)
	if f >= 0.5 {
		t.Fatalf("saturated channel should have low factor, got %v", f)
	}
	// 打满后不归零，保底 5%
	if f < headroomFloor {
		t.Fatalf("factor should be floored at %v, got %v", headroomFloor, f)
	}
}

func TestEmptyChannelKeepsFullWeightWithCap(t *testing.T) {
	Reset()
	for range 60 {
		RecordHit(3)
	}
	LearnCapFromRateLimit(3)
	// 未学过的渠道不受影响
	if f := HeadroomFactor(99); f != 1 {
		t.Fatalf("unknown channel factor should be 1, got %v", f)
	}
}
