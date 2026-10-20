package version

import "testing"

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func TestParse(t *testing.T) {
	for _, s := range []string{"8", "8.3", "8.3.14", "1.2.3.4", "0.0.0"} {
		if got := mustParse(t, s).String(); got != s {
			t.Errorf("Parse(%q).String() = %q", s, got)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{"", "8.", ".8", "8..3", "v8.3", "8.3.x", "08.3", "+8", "-8", "1.2.3.4.5", "8.3.14-rc1"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", s)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"8.3.14", "8.3.14", 0},
		{"8.3.9", "8.3.14", -1},
		{"8.10", "8.9", 1},
		{"8.3", "8.3.0", -1},
		{"7.4.33", "8.0.0", -1},
	}
	for _, tt := range tests {
		if got := Compare(mustParse(t, tt.a), mustParse(t, tt.b)); got != tt.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestHasPrefix(t *testing.T) {
	v := mustParse(t, "8.3.14")
	for _, p := range []string{"8", "8.3", "8.3.14"} {
		if !v.HasPrefix(mustParse(t, p)) {
			t.Errorf("%s should have prefix %s", v, p)
		}
	}
	for _, p := range []string{"8.31", "8.3.1", "8.3.14.0", "9"} {
		if v.HasPrefix(mustParse(t, p)) {
			t.Errorf("%s should not have prefix %s", v, p)
		}
	}
	if !v.HasPrefix(Version{}) {
		t.Errorf("the zero Version should be a prefix of %s", v)
	}
}
