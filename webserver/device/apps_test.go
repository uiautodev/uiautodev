package device

import "testing"

func TestParseAppScope(t *testing.T) {
	cases := []struct {
		input string
		want  AppScope
		ok    bool
	}{
		{"", AppScopeUser, true},
		{"user", AppScopeUser, true},
		{"system", AppScopeSystem, true},
		{"all", AppScopeAll, true},
		{"third_party", "", false},
		{"bogus", "", false},
	}
	for _, tc := range cases {
		got, err := ParseAppScope(tc.input)
		if tc.ok {
			if err != nil {
				t.Fatalf("ParseAppScope(%q) error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseAppScope(%q) = %q, want %q", tc.input, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Fatalf("ParseAppScope(%q) expected error", tc.input)
		}
	}
}
