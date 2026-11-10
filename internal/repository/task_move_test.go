package repository

import "testing"

func TestPositionBetween(t *testing.T) {
	ptr := func(v int64) *int64 { return &v }
	cases := []struct {
		name       string
		prev, next *int64
		want       int64
		ok         bool
	}{
		{"empty list", nil, nil, positionStep, true},
		{"after last", ptr(100), nil, 100 + positionStep, true},
		{"before first", nil, ptr(100), 100 - positionStep, true},
		{"middle", ptr(100), ptr(200), 150, true},
		{"narrowest gap", ptr(10), ptr(12), 11, true},
		{"adjacent", ptr(10), ptr(11), 0, false},
		{"equal", ptr(10), ptr(10), 0, false},
	}
	for _, tc := range cases {
		got, ok := positionBetween(tc.prev, tc.next)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: got (%d, %t), want (%d, %t)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}
