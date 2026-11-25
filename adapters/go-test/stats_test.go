package main

import "testing"

func TestTallyEvents(t *testing.T) {
	cases := []struct {
		name     string
		lines    []string
		executed int
		skipped  int
	}{
		{
			name:     "no events",
			executed: 0,
		},
		{
			name: "all leaves skip",
			lines: []string{
				`{"Action":"run","Test":"TestA"}`,
				`{"Action":"skip","Test":"TestA"}`,
				`{"Action":"run","Test":"TestB"}`,
				`{"Action":"skip","Test":"TestB"}`,
			},
			skipped: 2,
		},
		{
			name: "parent pass with skipped children",
			lines: []string{
				`{"Action":"run","Test":"TestParent"}`,
				`{"Action":"run","Test":"TestParent/one"}`,
				`{"Action":"skip","Test":"TestParent/one"}`,
				`{"Action":"run","Test":"TestParent/two"}`,
				`{"Action":"skip","Test":"TestParent/two"}`,
				`{"Action":"pass","Test":"TestParent"}`,
			},
			skipped: 2,
		},
		{
			name: "mixed and a real leaf",
			lines: []string{
				`{"Action":"run","Test":"TestOk"}`,
				`{"Action":"pass","Test":"TestOk"}`,
				`{"Action":"run","Test":"TestSkip"}`,
				`{"Action":"skip","Test":"TestSkip"}`,
			},
			executed: 1,
			skipped:  1,
		},
		{
			name: "leaf fail counts as executed",
			lines: []string{
				`{"Action":"run","Test":"TestBad"}`,
				`{"Action":"fail","Test":"TestBad"}`,
			},
			executed: 1,
		},
		{
			name: "parent without children",
			lines: []string{
				`{"Action":"run","Test":"TestSolo"}`,
				`{"Action":"pass","Test":"TestSolo"}`,
			},
			executed: 1,
		},
		{
			name: "package pass is not a test",
			lines: []string{
				`{"Action":"start","Package":"example"}`,
				`{"Action":"output","Package":"example","Output":"ok\texample\t0.1s [no tests to run]\n"}`,
				`{"Action":"pass","Package":"example"}`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tallyEvents(tc.lines)
			if got.executed != tc.executed || got.skipped != tc.skipped {
				t.Fatalf("executed=%d skipped=%d, want %d %d", got.executed, got.skipped, tc.executed, tc.skipped)
			}
		})
	}
}
