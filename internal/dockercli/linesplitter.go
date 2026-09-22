package dockercli

import "bytes"

// lineSplitter is an io.Writer that hands each complete line to emit as soon
// as it arrives. A process writes stderr in arbitrary chunks, so a line can
// span several Writes; the unterminated tail is held until its newline, or
// until Flush.
type lineSplitter struct {
	emit func([]byte)
	buf  []byte
}

func (l *lineSplitter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		l.deliver(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
}

// Flush delivers a final line the process did not end with a newline.
func (l *lineSplitter) Flush() {
	if len(l.buf) > 0 {
		l.deliver(l.buf)
		l.buf = nil
	}
}

func (l *lineSplitter) deliver(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if len(line) == 0 {
		return
	}
	// buf's backing array is reused by later Writes, so hand over a copy.
	l.emit(append([]byte(nil), line...))
}
