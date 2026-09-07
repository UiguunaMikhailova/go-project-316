package main

import (
	"testing"
	"time"
)

func TestRateDelay(t *testing.T) {
	cases := []struct {
		name  string
		rps   float64
		delay time.Duration
		want  time.Duration
	}{
		{name: "no limit", want: 0},
		{name: "delay only", delay: 200 * time.Millisecond, want: 200 * time.Millisecond},
		{name: "rps only", rps: 5, want: 200 * time.Millisecond},
		{name: "rps wins over delay", rps: 4, delay: time.Second, want: 250 * time.Millisecond},
		{name: "fractional rps", rps: 0.5, want: 2 * time.Second},
		{name: "negative delay", delay: -time.Second, want: 0},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := rateDelay(testCase.rps, testCase.delay); got != testCase.want {
				t.Errorf("rateDelay(%v, %v) = %v, want %v", testCase.rps, testCase.delay, got, testCase.want)
			}
		})
	}
}
