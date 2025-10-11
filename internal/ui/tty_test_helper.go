package ui

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/creack/pty"
)

func openPTYOrSkip(t *testing.T) (*os.File, *os.File) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("unable to open pseudo terminal: %v", err)
	}
	return master, slave
}

func discardPTY(r *os.File) {
	go func() {
		if _, err := io.Copy(io.Discard, r); err != nil && !errors.Is(err, os.ErrClosed) {
			return
		}
	}()
}
