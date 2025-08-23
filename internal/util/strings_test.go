package util

import "testing"

func TestSplitNonEmptyLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{}},
		{"only newlines", "\n\n\n", []string{}},
		{"single line", "hello", []string{"hello"}},
		{"trims spaces", "  a  \n  b\t\n\t\n c ", []string{"a", "b", "c"}},
		{"keeps order", "one\n\n two \nthree\n", []string{"one", "two", "three"}},
		{"leading and trailing blanks", "\n  x\n\n y \n\n", []string{"x", "y"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitNonEmptyLines(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("idx %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"equal to max", "helloworld", 10, "helloworld"},
		{"longer than max", "helloworld", 5, "hello..."},
		{"zero max", "abc", 0, "..."},
		{"one char max", "abcdef", 1, "a..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.s, tc.max)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
