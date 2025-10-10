package dockercli

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
)

// PsJSONRow represents a single line of `docker ps --format {{json .}}` output.
// Only the fields we need are included; additional fields are ignored by json.Unmarshal.
type PsJSONRow struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

// PsJSON returns docker ps entries as parsed rows. When all is true, includes stopped containers (-a).
// Filters are passed as raw values for repeated --filter arguments, e.g.,
//
//	["label=io.dockform.identifier=myid", "label=com.docker.compose.service=web"]
func (c *Client) PsJSON(ctx context.Context, all bool, filters []string) ([]PsJSONRow, error) {
	args := []string{"ps", "--format", "{{json .}}"}
	if all {
		args = append(args, "-a")
	}
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		args = append(args, "--filter", f)
	}
	out, err := c.exec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	rows := make([]PsJSONRow, 0)
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var row PsJSONRow
		if json.Unmarshal([]byte(line), &row) == nil {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// LabelValue parses a comma-separated label string (as returned by docker ps) and returns the value for key.
func (r PsJSONRow) LabelValue(key string) string {
	labels := strings.Split(r.Labels, ",")
	for _, kv := range labels {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		if !strings.Contains(kv, "=") {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := ""
		if len(parts) > 1 {
			v = strings.TrimSpace(parts[1])
		}
		if k == key {
			return v
		}
	}
	return ""
}
