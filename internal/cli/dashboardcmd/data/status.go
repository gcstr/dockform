package data

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gcstr/dockform/internal/dockercli"
)

// StatusProvider resolves container names for services and fetches their docker ps status.
type StatusProvider struct {
	docker     *dockercli.Client
	identifier string // io.dockform.identifier
}

func NewStatusProvider(d *dockercli.Client, identifier string) *StatusProvider {
	return &StatusProvider{docker: d, identifier: strings.TrimSpace(identifier)}
}

// Docker exposes the underlying docker client; used by the TUI to stream logs.
func (sp *StatusProvider) Docker() *dockercli.Client { return sp.docker }

// Key uniquely identifies a service status entry in the dashboard list.
type Key struct {
	Stack   string
	Service string
}

type Status struct {
	ContainerName string
	State         string // running, exited, restarting, created, etc.
	StatusText    string // e.g., "Up 13 days (healthy)" or "Exited (0) 2 hours ago"
}

// ResolveContainerName chooses the container name for a service.
// Prefer an explicit container name from compose; otherwise, a best-effort lookup by labels.
func (sp *StatusProvider) ResolveContainerName(ctx context.Context, stackName string, svc ServiceSummary) (string, error) {
	name := strings.TrimSpace(svc.ContainerName)
	if name != "" {
		return name, nil
	}
	filters := []string{}
	if sp.identifier != "" {
		filters = append(filters, "label=io.dockform.identifier="+sp.identifier)
	}
	if strings.TrimSpace(svc.Service) != "" {
		filters = append(filters, "label=com.docker.compose.service="+svc.Service)
	}
	rows, err := sp.docker.PsJSON(ctx, true, filters)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	// Pick a stable result: sort by Names asc and choose first
	sort.Slice(rows, func(i, j int) bool { return rows[i].Names < rows[j].Names })
	return rows[0].Names, nil
}

// FetchAll returns a map from (stack, service) to Status. Unknown entries are omitted.
func (sp *StatusProvider) FetchAll(ctx context.Context, stacks []StackSummary) (map[Key]Status, error) {
	out := make(map[Key]Status)
	// Build filters once for this manifest (identifier limits scope)
	filters := []string{}
	if sp.identifier != "" {
		filters = append(filters, "label=io.dockform.identifier="+sp.identifier)
	}
	rows, err := sp.docker.PsJSON(ctx, true, filters)
	if err != nil {
		return nil, err
	}
	// Index rows by name and by compose service for fallback
	byName := map[string]dockercli.PsJSONRow{}
	byService := map[string]dockercli.PsJSONRow{}
	for _, r := range rows {
		name := strings.TrimSpace(r.Names)
		if name != "" {
			byName[name] = r
		}
		if svc := strings.TrimSpace(r.LabelValue("com.docker.compose.service")); svc != "" {
			byService[svc] = r
		}
	}
	// Walk desired services
	for _, st := range stacks {
		for _, svc := range st.Services {
			key := Key{Stack: st.Name, Service: svc.Service}
			cand := dockercli.PsJSONRow{}
			if cname := strings.TrimSpace(svc.ContainerName); cname != "" {
				if r, ok := byName[cname]; ok {
					cand = r
				}
			} else if r, ok := byService[strings.TrimSpace(svc.Service)]; ok {
				cand = r
			}
			if strings.TrimSpace(cand.Names) == "" {
				continue // not found
			}
			out[key] = Status{
				ContainerName: cand.Names,
				State:         strings.TrimSpace(cand.State),
				StatusText:    strings.TrimSpace(cand.Status),
			}
		}
	}
	return out, nil
}

// FormatStatusLine produces the "● <StatusText>" content and chooses a color key.
// Returns (bulletColor, text) where bulletColor is one of: success, warning, error.
func FormatStatusLine(state string, statusText string) (string, string) {
	st := strings.ToLower(strings.TrimSpace(state))
	var sel string
	switch st {
	case "running":
		// Determine health from status text
		low := strings.ToLower(statusText)
		if strings.Contains(low, "(healthy)") || !strings.Contains(low, "(unhealthy)") && !strings.Contains(low, "(starting)") {
			sel = "success"
		} else if strings.Contains(low, "(starting)") {
			sel = "warning"
		} else {
			sel = "error"
		}
	case "restarting", "created":
		sel = "warning"
	default:
		sel = "error"
	}
	return sel, strings.TrimSpace(statusText)
}

// ColorStyle returns the ANSI-colored bullet given a color key and a plain bullet symbol.
func ColorStyle(colorKey string, bullet string) string {
	switch colorKey {
	case "success":
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", 0x12, 0xC7, 0x8F, bullet) // theme.Success
	case "warning":
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", 0xE8, 0xFE, 0x96, bullet) // theme.Warning
	default:
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", 0xEB, 0x42, 0x68, bullet) // theme.Error
	}
}
