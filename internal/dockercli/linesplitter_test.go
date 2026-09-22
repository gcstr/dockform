package dockercli

import (
	"reflect"
	"testing"
)

func collectLines(writes ...string) (*lineSplitter, *[]string) {
	var got []string
	ls := &lineSplitter{emit: func(line []byte) { got = append(got, string(line)) }}
	for _, w := range writes {
		_, _ = ls.Write([]byte(w))
	}
	return ls, &got
}

// A process writes stderr in arbitrary chunks, so one line can span Writes.
func TestLineSplitter_JoinsLinesAcrossWrites(t *testing.T) {
	_, got := collectLines("alpha\nbe", "ta\n")
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %q, want %q", *got, want)
	}
}

func TestLineSplitter_FlushDeliversUnterminatedTail(t *testing.T) {
	ls, got := collectLines("alpha\nbeta")
	if want := []string{"alpha"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("before flush: got %q, want %q", *got, want)
	}
	ls.Flush()
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("after flush: got %q, want %q", *got, want)
	}
}

func TestLineSplitter_DropsBlankLinesAndCarriageReturns(t *testing.T) {
	_, got := collectLines("alpha\r\n\n\r\nbeta\n")
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %q, want %q", *got, want)
	}
}

// emit may keep the slice it is handed; later Writes must not change it.
func TestLineSplitter_HandsOverIndependentCopies(t *testing.T) {
	var kept [][]byte
	ls := &lineSplitter{emit: func(line []byte) { kept = append(kept, line) }}
	_, _ = ls.Write([]byte("first\n"))
	_, _ = ls.Write([]byte("second\n"))
	if string(kept[0]) != "first" {
		t.Fatalf("first line was overwritten: %q", kept[0])
	}
}
