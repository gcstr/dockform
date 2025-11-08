package logger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	clog "github.com/charmbracelet/log"
	"github.com/mattn/go-isatty"
)

// Logger is a small facade over the underlying logging backend.
// Methods accept a message (event name in snake_case) and structured key/value fields.
type Logger interface {
	Debug(msg string, keyvals ...any)
	Info(msg string, keyvals ...any)
	Warn(msg string, keyvals ...any)
	Error(msg string, keyvals ...any)
	With(keyvals ...any) Logger
}

// Options controls logger construction.
type Options struct {
	// Out is the primary destination for human-facing logs. Defaults to os.Stderr.
	Out io.Writer
	// Level is one of: "debug", "info", "warn", "error". Defaults to "info".
	Level string
	// Format controls primary output: "auto" (default), "pretty", or "json".
	// When "auto", TTY → pretty; non-TTY → json.
	Format string
	// NoColor disables color in pretty output. For JSON it has no effect.
	NoColor bool
	// LogFile, when set, enables an additional JSON sink written to this path.
	LogFile string
	// ReportTimestamp toggles timestamps on the primary sink. Default: true.
	ReportTimestamp *bool
}

// New constructs a Logger according to Options. It may create an additional
// file sink when Options.LogFile is provided. The returned closer should be
// invoked on process exit to flush/close any resources (it is a no-op if nil).
func New(opts Options) (Logger, io.Closer, error) {
	primaryOut := opts.Out
	if primaryOut == nil {
		primaryOut = os.Stderr
	}

	// Build primary sink
	var primary Logger
	{
		formatter := chooseFormatter(primaryOut, opts.Format)
		lvl := parseLevel(opts.Level)
		cl := clog.NewWithOptions(primaryOut, clog.Options{})
		cl.SetLevel(lvl)
		cl.SetFormatter(formatter)
		if opts.ReportTimestamp == nil || *opts.ReportTimestamp {
			cl.SetReportTimestamp(true)
		} else {
			cl.SetReportTimestamp(false)
		}
		if opts.NoColor {
			// Best-effort: many Charm libs respect NO_COLOR; set it here.
			_ = os.Setenv("NO_COLOR", "1")
		}
		primary = &charmLogger{l: cl}
	}

	// Optional file sink
	var closer io.Closer
	var sinks []Logger
	sinks = append(sinks, primary)
	if strings.TrimSpace(opts.LogFile) != "" {
		f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
		fl := clog.NewWithOptions(f, clog.Options{})
		fl.SetLevel(parseLevel(opts.Level))
		fl.SetFormatter(chooseFormatter(f, opts.Format))
		// File logs default to no timestamps for machine parsing (unless pretty format is explicitly requested)
		fl.SetReportTimestamp(opts.Format == "pretty" || opts.Format == "text")
		sinks = append(sinks, &charmLogger{l: fl})
		closer = f
	}

	if len(sinks) == 1 {
		return sinks[0], closer, nil
	}
	return &multiLogger{sinks: sinks}, closer, nil
}

func chooseFormatter(w io.Writer, format string) clog.Formatter {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return clog.JSONFormatter
	case "pretty", "text":
		return clog.TextFormatter
	default:
		if f, ok := w.(*os.File); ok {
			if isatty.IsTerminal(f.Fd()) {
				return clog.TextFormatter
			}
		}
		return clog.JSONFormatter
	}
}

func parseLevel(s string) clog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return clog.DebugLevel
	case "warn", "warning":
		return clog.WarnLevel
	case "error":
		return clog.ErrorLevel
	default:
		return clog.InfoLevel
	}
}

type charmLogger struct{ l *clog.Logger }

func (c *charmLogger) Debug(msg string, keyvals ...any) { c.l.Debug(msg, redactPairs(keyvals)...) }
func (c *charmLogger) Info(msg string, keyvals ...any)  { c.l.Info(msg, redactPairs(keyvals)...) }
func (c *charmLogger) Warn(msg string, keyvals ...any)  { c.l.Warn(msg, redactPairs(keyvals)...) }
func (c *charmLogger) Error(msg string, keyvals ...any) { c.l.Error(msg, redactPairs(keyvals)...) }
func (c *charmLogger) With(keyvals ...any) Logger {
	return &charmLogger{l: c.l.With(redactPairs(keyvals)...)}
}

type multiLogger struct{ sinks []Logger }

func (m *multiLogger) Debug(msg string, keyvals ...any) {
	for _, s := range m.sinks {
		s.Debug(msg, keyvals...)
	}
}
func (m *multiLogger) Info(msg string, keyvals ...any) {
	for _, s := range m.sinks {
		s.Info(msg, keyvals...)
	}
}
func (m *multiLogger) Warn(msg string, keyvals ...any) {
	for _, s := range m.sinks {
		s.Warn(msg, keyvals...)
	}
}
func (m *multiLogger) Error(msg string, keyvals ...any) {
	for _, s := range m.sinks {
		s.Error(msg, keyvals...)
	}
}
func (m *multiLogger) With(keyvals ...any) Logger {
	next := make([]Logger, 0, len(m.sinks))
	for _, s := range m.sinks {
		next = append(next, s.With(keyvals...))
	}
	return &multiLogger{sinks: next}
}

// Fanout returns a Logger that forwards every log call to all provided sinks.
// If only one sink is provided, that sink is returned unchanged.
// This does not modify the behavior or configuration of the provided loggers;
// it simply aggregates them.
func Fanout(sinks ...Logger) Logger {
	if len(sinks) == 0 {
		return Nop()
	}
	if len(sinks) == 1 {
		return sinks[0]
	}
	// Flatten nested multiLoggers to avoid deep chains
	flat := make([]Logger, 0, len(sinks))
	for _, s := range sinks {
		if ml, ok := s.(*multiLogger); ok {
			flat = append(flat, ml.sinks...)
			continue
		}
		flat = append(flat, s)
	}
	return &multiLogger{sinks: flat}
}

// Step is a helper for emitting started/ok/failed events with consistent keys.
type Step struct {
	logger   Logger
	action   string // snake_case event name, e.g. "network_ensure"
	resource string // resource name, e.g. network name
	started  time.Time
	base     []any // pre-attached stable fields
}

// StartStep logs a started event and returns a Step that can be finalized with OK/Fail.
// Stable keys: action, target, status, changed, duration_ms.
func StartStep(l Logger, action string, resource string, extra ...any) *Step {
	s := &Step{logger: l, action: action, resource: resource, started: time.Now(), base: redactPairs(extra)}
	fields := append([]any{
		"status", "started",
		"action", action,
		"resource", resource,
	}, s.base...)
	s.logger.Info(action, fields...)
	return s
}

// OK marks the step as successful. Provide whether a change occurred and any extra fields.
func (s *Step) OK(changed bool, extra ...any) {
	dur := time.Since(s.started).Milliseconds()
	fields := append([]any{
		"status", "ok",
		"action", s.action,
		"resource", s.resource,
		"changed", changed,
		"duration_ms", dur,
	}, redactPairs(extra)...)
	s.logger.Info(s.action, fields...)
}

// Fail logs the failure once with error details and returns the provided error unchanged.
func (s *Step) Fail(err error, extra ...any) error {
	dur := time.Since(s.started).Milliseconds()
	msg := ""
	if err != nil {
		msg = redactError(err)
	}
	fields := append([]any{
		"status", "failed",
		"action", s.action,
		"resource", s.resource,
		"changed", false,
		"duration_ms", dur,
	}, redactPairs(extra)...)
	if msg != "" {
		fields = append(fields, "error", msg)
	}
	s.logger.Error(s.action, fields...)
	return err
}

// Redaction ---------------------------------------------------------------

// redactPairs scrubs sensitive values in k/v pairs. Keys containing the
// sensitive substrings will have their value replaced with "[REDACTED]".
func redactPairs(kv []any) []any {
	if len(kv) == 0 {
		return kv
	}
	out := make([]any, len(kv))
	copy(out, kv)
	for i := 0; i+1 < len(out); i += 2 {
		key, ok := out[i].(string)
		if !ok {
			continue
		}
		if isSensitiveKey(key) {
			out[i+1] = "[REDACTED]"
		} else if v, ok := out[i+1].(string); ok {
			out[i+1] = redactText(v)
		}
	}
	return out
}

func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	return strings.Contains(lower, "password") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "private") ||
		strings.Contains(lower, "key") && !strings.Contains(lower, "keyboard")
}

var secretLike = regexp.MustCompile(`(?i)(token|secret|password|apikey|api_key|bearer)\s*[:=]\s*([A-Za-z0-9\-\._]+)`) // very loose

func redactText(s string) string {
	return secretLike.ReplaceAllString(s, "$1=[REDACTED]")
}

func redactError(err error) string {
	if err == nil {
		return ""
	}
	return redactText(err.Error())
}

// Context -----------------------------------------------------------------

type ctxKey struct{}

// WithContext returns a derived context carrying the logger.
func WithContext(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger from context or a no-op logger if absent.
func FromContext(ctx context.Context) Logger {
	if ctx == nil {
		return Nop()
	}
	if v := ctx.Value(ctxKey{}); v != nil {
		if l, ok := v.(Logger); ok && l != nil {
			return l
		}
	}
	return Nop()
}

// Nop returns a Logger that discards all logs.
func Nop() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
func (nopLogger) With(...any) Logger   { return nopLogger{} }

// NewRunID generates a random 12-hex-character run identifier.
func NewRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "run-000000"
	}
	return hex.EncodeToString(b[:])
}

// Example usage (documentation-only):
//
//  l, _, _ := logger.New(logger.Options{Format: "json", Level: "info"})
//  cmdLog := l.With("run_id", logger.NewRunID(), "command", "plan")
//  netLog := cmdLog.With("component", "network")
//  st := logger.StartStep(netLog, "network_ensure", "df_net")
//  // ... perform operation ...
//  st.OK(true)
