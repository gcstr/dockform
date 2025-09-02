package ui

import (
    "bytes"
    "strings"
    "testing"
)

func TestStripANSI_RemovesCodes(t *testing.T) {
    in := "\x1b[31mred\x1b[0m and normal"
    got := StripANSI(in)
    if got != "red and normal" {
        t.Fatalf("expected ANSI to be stripped, got: %q", got)
    }
}

func TestStdPrinter_WritesToCorrectStreams_WithPrefixes(t *testing.T) {
    var out bytes.Buffer
    var err bytes.Buffer
    p := StdPrinter{Out: &out, Err: &err}

    p.Plain("hello %s", "plain")
    p.Info("hello %s", "world")
    p.Warn("warn %d", 1)
    p.Error("error %s", "boom")

    outStr := StripANSI(out.String())
    errStr := StripANSI(err.String())

    if !strings.Contains(outStr, "hello plain\n") {
        t.Fatalf("expected plain text on stdout, got: %q", outStr)
    }
    if !strings.Contains(outStr, "[info] hello world\n") {
        t.Fatalf("expected info prefix on stdout, got: %q", outStr)
    }
    if !strings.Contains(errStr, "[warn] warn 1\n") {
        t.Fatalf("expected warn prefix on stderr, got: %q", errStr)
    }
    if !strings.Contains(errStr, "[error] error boom\n") {
        t.Fatalf("expected error prefix on stderr, got: %q", errStr)
    }
}

func TestRenderSectionedList_ShowsItemsWithIcons(t *testing.T) {
    sections := []Section{
        {
            Title: "Applications",
            Items: []DiffLine{
                Line(Noop, "noop item"),
                Line(Add, "add item"),
                Line(Remove, "remove item"),
                Line(Change, "change item"),
            },
        },
        { // empty section is skipped
            Title: "Empty",
            Items: nil,
        },
    }
    got := StripANSI(RenderSectionedList(sections))
    // Ensure messages are present
    for _, s := range []string{"noop item", "add item", "remove item", "change item"} {
        if !strings.Contains(got, s) {
            t.Fatalf("expected sectioned list to contain %q, got: %q", s, got)
        }
    }
    // Ensure icon glyphs are present
    for _, icon := range []string{"●", "↑", "↓", "→"} {
        if !strings.Contains(got, icon) {
            t.Fatalf("expected sectioned list to contain icon %q, got: %q", icon, got)
        }
    }
}

func TestSpinner_NoTTY_NoOutput(t *testing.T) {
    var out bytes.Buffer
    s := NewSpinner(&out, "Loading...")
    s.Start()
    s.Stop()
    if out.Len() != 0 {
        t.Fatalf("expected spinner to produce no output when not a TTY, got: %q", out.String())
    }
}

func TestProgress_NoTTY_NoOutput(t *testing.T) {
    var out bytes.Buffer
    p := NewProgress(&out, "Applying")
    p.Start(3)
    p.Increment()
    p.SetAction("doing work")
    p.Stop()
    if out.Len() != 0 {
        t.Fatalf("expected progress to produce no output when not a TTY, got: %q", out.String())
    }
}

