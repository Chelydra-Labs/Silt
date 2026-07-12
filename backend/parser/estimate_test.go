package parser

import "testing"

func TestParseEstimateMinutes(t *testing.T) {
	cases := []struct {
		raw    string
		want   int
		wantOK bool
	}{
		{"30m", 30, true},
		{"2h", 120, true},
		{"1d", 480, true},
		{"2.5d", 1200, true},
		{"1.5h", 90, true},
		{"  45 M  ", 45, true},
		{"0m", 0, false},
		{"0h", 0, false},
		{"0d", 0, false},
		{"", 0, false},
		{"   ", 0, false},
		{"abc", 0, false},
		{"30", 0, false}, // bare number rejected
		{"-1h", 0, false},
		{"2x", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseEstimateMinutes(c.raw)
		if ok != c.wantOK || got != c.want {
			t.Errorf("ParseEstimateMinutes(%q) = (%d, %v), want (%d, %v)", c.raw, got, ok, c.want, c.wantOK)
		}
	}
}

func TestFormatEstimateMinutes(t *testing.T) {
	cases := []struct {
		mins int
		want string
	}{
		{0, ""},
		{-1, ""},
		{30, "30m"},
		{60, "1h"},
		{90, "1.5h"},
		{480, "1d"},
		{960, "2d"},
		{1200, "2.5d"}, // 2.5 work-days — not "20h"
		{720, "1.5d"},
		{540, "9h"},  // not "1.125d"
		{600, "10h"}, // not "1.25d"
		{240, "4h"},
	}
	for _, c := range cases {
		if got := FormatEstimateMinutes(c.mins); got != c.want {
			t.Errorf("FormatEstimateMinutes(%d) = %q, want %q", c.mins, got, c.want)
		}
	}
}
