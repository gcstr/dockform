package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Spinner renders a simple TTY spinner with an optional label.
// It is disabled automatically when the writer is not a terminal.
type Spinner struct {
	out     io.Writer
	label   string
	style   lipgloss.Style
	frames  []string
	delay   time.Duration
	enabled bool

	// spacer ensures a blank line above the spinner while running
	spacerAdded bool

	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.Mutex
}

// NewSpinner creates a new spinner that writes to out with the given label.
// The spinner only animates if out is a TTY; otherwise Start/Stop are no-ops.
func NewSpinner(out io.Writer, label string) *Spinner {
	enabled := false
	if f, ok := out.(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		enabled = true
	}
	return &Spinner{
		out:     out,
		label:   label,
		style:   lipgloss.NewStyle().Foreground(lipgloss.Color("69")),
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		delay:   100 * time.Millisecond,
		enabled: enabled,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start begins rendering the spinner. Calling Start multiple times is safe.
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || s.stopCh == nil || s.doneCh == nil {
		return
	}
	// Insert a visual spacer line before spinner so it doesn't hug previous output
	if !s.spacerAdded {
		_, _ = fmt.Fprint(s.out, "\n")
		s.spacerAdded = true
	}
	// If already running, do nothing
	select {
	case <-s.doneCh:
		// finished previously; recreate channels for another run
		s.stopCh = make(chan struct{})
		s.doneCh = make(chan struct{})
	default:
	}

	go func(stop <-chan struct{}, done chan<- struct{}) {
		ticker := time.NewTicker(s.delay)
		defer func() {
			ticker.Stop()
			// Clear line
			_, _ = fmt.Fprint(s.out, "\r\x1b[2K")
			close(done)
		}()
		i := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				frame := s.frames[i%len(s.frames)]
				i++
				// Render without newline; carriage return to rewrite line
				// Ensure one space before and after the spinner
				_, _ = fmt.Fprintf(s.out, "\r %s %s", s.style.Render(frame), s.label)
			}
		}
	}(s.stopCh, s.doneCh)
}

// Stop stops the spinner and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || s.stopCh == nil || s.doneCh == nil {
		return
	}
	// Signal stop
	select {
	case <-s.doneCh:
		// already stopped
	default:
		close(s.stopCh)
		<-s.doneCh
	}
	// After the spinner line is cleared by the goroutine, also remove the spacer line
	if s.spacerAdded {
		// Move cursor up one line and clear it
		_, _ = fmt.Fprint(s.out, "\x1b[1A\x1b[2K")
		s.spacerAdded = false
	}
}
