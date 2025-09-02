package ui

import (
    "bytes"
    "testing"
)

func TestConfirmYesTTY_AcceptsYes(t *testing.T) {
    in := bytes.NewBufferString("yes\n")
    var out bytes.Buffer
    ok, entered, err := ConfirmYesTTY(in, &out)
    if err != nil {
        t.Fatalf("confirm prompt error: %v", err)
    }
    if !ok || entered != "yes" {
        t.Fatalf("expected ok=true and entered=\"yes\", got ok=%v entered=%q", ok, entered)
    }
}

func TestConfirmYesTTY_RejectsNonYes(t *testing.T) {
    in := bytes.NewBufferString("no\n")
    var out bytes.Buffer
    ok, entered, err := ConfirmYesTTY(in, &out)
    if err != nil {
        t.Fatalf("confirm prompt error: %v", err)
    }
    if ok || entered != "no" {
        t.Fatalf("expected ok=false and entered=\"no\", got ok=%v entered=%q", ok, entered)
    }
}

