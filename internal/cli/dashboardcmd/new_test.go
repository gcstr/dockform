package dashboardcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/v2/list"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/components"
	"github.com/gcstr/dockform/internal/cli/dashboardcmd/data"
)

func TestPresentServiceLinesIncludesContainer(t *testing.T) {
	// With the new UI, presentServiceLines returns only the service and image; the
	// delegate chooses to display the container name when available.
	lines := presentServiceLines(data.ServiceSummary{Service: "web", ContainerName: "web-prod", Image: "nginx:alpine"})
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "web" {
		t.Fatalf("expected first line to be service only, got %q", lines[0])
	}
	if lines[1] != "nginx:alpine" {
		t.Fatalf("expected image line to be raw image name, got %q", lines[1])
	}
}

func TestPresentServiceLinesWithoutContainer(t *testing.T) {
	lines := presentServiceLines(data.ServiceSummary{Service: "web", Image: "nginx:alpine"})
	if lines[0] != "web" {
		t.Fatalf("expected service name only when container empty, got %q", lines[0])
	}
}

func TestStackItemsFromSummariesCreatesPerServiceItems(t *testing.T) {
	summaries := []data.StackSummary{
		{
			Name: "paperless",
			Services: []data.ServiceSummary{
				{Service: "paperless-ngx", ContainerName: "paperless-ngx", Image: "paperlessngx/paperless-ngx:2.18.4"},
				{Service: "paperless-redis", ContainerName: "paperless-redis", Image: "redis:8"},
			},
		},
	}
	items := stackItemsFromSummaries(summaries)
	if len(items) != 2 {
		t.Fatalf("expected one item per service, got %d", len(items))
	}
	for idx, item := range items {
		stackItem, ok := item.(components.StackItem)
		if !ok {
			t.Fatalf("item %d not a StackItem", idx)
		}
		if stackItem.TitleText != "paperless" {
			t.Fatalf("expected stack title preserved, got %q", stackItem.TitleText)
		}
		if stackItem.Status != "○ status unknown" {
			t.Fatalf("unexpected status %q", stackItem.Status)
		}
		if stackItem.FilterText == "" {
			t.Fatalf("expected filter text to be populated")
		}
	}
}

func TestStackItemsFromSummariesNoServices(t *testing.T) {
	items := stackItemsFromSummaries([]data.StackSummary{{Name: "empty"}})
	if len(items) != 1 {
		t.Fatalf("expected placeholder item, got %d", len(items))
	}
	stackItem := items[0].(components.StackItem)
	if stackItem.Status != "○ no services" {
		t.Fatalf("unexpected status %q", stackItem.Status)
	}
	if len(stackItem.Containers) != 1 || stackItem.Containers[0] != "(no services)" {
		t.Fatalf("expected placeholder containers, got %+v", stackItem.Containers)
	}
}

func TestBuildFilterValueTrimsBlanks(t *testing.T) {
	filter := buildFilterValue("stack", data.ServiceSummary{Service: "svc", ContainerName: "", Image: "image"})
	if filter != "stack svc image" {
		t.Fatalf("expected concatenated filter without double spaces, got %q", filter)
	}
}

func TestRenderServiceStatusConstant(t *testing.T) {
	if got := renderServiceStatus(data.ServiceSummary{}); got != "○ status unknown" {
		t.Fatalf("unexpected status %q", got)
	}
}

func TestComputeColumnWidthsLargeScreen(t *testing.T) {
	left, center, right := computeColumnWidths(150)
	if left != 40 || right != 30 || center != 72 {
		t.Fatalf("unexpected widths: left=%d center=%d right=%d", left, center, right)
	}
}

func TestComputeColumnWidthsTinyScreen(t *testing.T) {
	left, center, right := computeColumnWidths(0)
	if left != 1 || center != 1 || right != 1 {
		t.Fatalf("expected minimum widths when total non-positive, got %d %d %d", left, center, right)
	}
}

func TestRenderSlashBannerProducesThreeLines(t *testing.T) {
	width := 20
	banner := renderSlashBanner(width, "dockform")
	lines := strings.Split(banner, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected banner to have 3 lines, got %d", len(lines))
	}
	for idx, line := range lines {
		if lipgloss.Width(line) != width {
			t.Fatalf("line %d has width %d, expected %d", idx, lipgloss.Width(line), width)
		}
	}
}

func TestNewModelCreatesListWithItems(t *testing.T) {
	summaries := []data.StackSummary{{Name: "stack", Services: []data.ServiceSummary{{Service: "svc", Image: "img"}}}}
	m := newModel(context.Background(), nil, summaries, "1.2.3", "demo", "/tmp/dockform.yml", "default", "unix:///var/run/docker.sock", "24.0.0")
	if m.quitting {
		t.Fatalf("model should start non-quitting")
	}
	if items := m.list.VisibleItems(); len(items) == 0 {
		t.Fatalf("expected list to have visible items")
	}
	if got := m.list.FilterState(); got != list.Unfiltered {
		t.Fatalf("expected list to start unfiltered, got state %v", got)
	}
}

func TestTruncateLeftAddsEllipsis(t *testing.T) {
	path := "/Users/example/workspace/dockform/manifest.yaml"
	got := truncateLeft(path, 18)
	if !strings.HasPrefix(got, "...") {
		t.Fatalf("expected prefix ellipsis, got %q", got)
	}
	if !strings.HasSuffix(got, "manifest.yaml") {
		t.Fatalf("expected to keep file suffix, got %q", got)
	}
}

func TestDisplayContextNameFallback(t *testing.T) {
	if got := displayContextName(" "); got != "default" {
		t.Fatalf("expected default fallback, got %q", got)
	}
}

func TestDisplayDockerHostFallback(t *testing.T) {
	if got := displayDockerHost(""); got != "(unknown)" {
		t.Fatalf("expected unknown fallback, got %q", got)
	}
}

func TestDisplayEngineVersionFallback(t *testing.T) {
	if got := displayEngineVersion(""); got != "(unknown)" {
		t.Fatalf("expected unknown fallback, got %q", got)
	}
}

func TestDisplayVolumeMountFallback(t *testing.T) {
	if got := displayVolumeMount(" "); got != "(no mountpoint)" {
		t.Fatalf("expected mountpoint fallback, got %q", got)
	}
}

func TestDisplayVolumeDriverFallback(t *testing.T) {
	if got := displayVolumeDriver(" "); got != "(driver unknown)" {
		t.Fatalf("expected driver fallback, got %q", got)
	}
}
