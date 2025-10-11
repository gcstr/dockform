package dashboardcmd

import "testing"

func TestStreamLogsCmdWithoutProvider(t *testing.T) {
	m := newDashboardModel()
	m.statusProvider = nil
	if cmd := m.streamLogsCmd("container"); cmd != nil {
		t.Fatalf("expected nil command when status provider missing")
	}
	m.logLines = make(chan string, 2)
	m.logLines <- "entry-1"
	m.logLines <- "entry-2"
	m = m.withFlushedLogs()
	if len(m.logsBuf) != 2 {
		t.Fatalf("expected buffered logs to flush")
	}
}
