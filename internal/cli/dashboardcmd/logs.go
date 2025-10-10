package dashboardcmd

import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
)

func (m model) tickLogs() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return logsTickMsg{} })
}

type logsTickMsg struct{}
type startLogsFor struct{ name string }
type logStreamStartedMsg struct{ cancel context.CancelFunc }

func (m *model) streamLogsCmd(name string) tea.Cmd {
	if m.statusProvider == nil {
		return nil
	}
	pr, pw := io.Pipe()
    ctxParent := m.ctx
    if ctxParent == nil {
        ctxParent = context.Background()
    }
    ctx, cancel := context.WithCancel(ctxParent)
	if m.logLines == nil {
		m.logLines = make(chan string, 256)
	}
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			m.logLines <- sc.Text()
		}
	}()
	go func() {
		_ = m.statusProvider.Docker().StreamContainerLogs(ctx, name, 300, "", pw)
		_ = pw.Close()
	}()
	return func() tea.Msg { return logStreamStartedMsg{cancel: cancel} }
}

func (m *model) withFlushedLogs() model {
	drained := false
	for m.logLines != nil {
		select {
		case ln := <-m.logLines:
			m.logsBuf = append(m.logsBuf, ln)
			if len(m.logsBuf) > 1000 {
				m.logsBuf = m.logsBuf[len(m.logsBuf)-1000:]
			}
			drained = true
		default:
			goto done
		}
	}
done:
	if drained {
		m.logsPager.SetContent(strings.Join(m.logsBuf, "\n"))
	}
	return *m
}
