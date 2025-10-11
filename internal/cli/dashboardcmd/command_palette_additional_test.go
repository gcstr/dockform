package dashboardcmd

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/bubbles/v2/list"
)

func TestCommandItemAccessors(t *testing.T) {
	item := commandItem{id: "pause", title: "Pause"}
	if item.Title() != "Pause" || item.Description() != "" || item.FilterValue() != "pause" {
		t.Fatalf("unexpected command item accessors")
	}
}

func TestCommandDelegateRender(t *testing.T) {
	items := []list.Item{commandItem{id: "pause", title: "Pause"}}
	l := list.New(items, commandDelegate{}, 0, 0)
	var buf bytes.Buffer
	delegate := commandDelegate{}
	delegate.Update(nil, &l)
	delegate.Render(&buf, l, 0, items[0])
	if buf.Len() == 0 {
		t.Fatalf("expected rendered output")
	}
}

func TestKeysHelpFunctions(t *testing.T) {
	k := newKeyMap()
	if len(k.ShortHelp()) == 0 {
		t.Fatalf("expected short help entries")
	}
	if len(k.FullHelp()) == 0 {
		t.Fatalf("expected full help entries")
	}
}
