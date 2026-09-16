package planner

import "testing"

func TestSectionTitle_CoversEveryResourceType(t *testing.T) {
	cases := map[ResourceType]string{
		ResourceVolume:    "Volumes",
		ResourceNetwork:   "Networks",
		ResourceStack:     "Stacks",
		ResourceService:   "Stacks", // services nest under their stack's section
		ResourceFileset:   "Filesets",
		ResourceFile:      "Filesets", // files nest under their fileset's section
		ResourceContainer: "Containers",
	}
	for typ, want := range cases {
		if got := SectionTitle(typ); got != want {
			t.Errorf("SectionTitle(%v) = %q, want %q", typ, got, want)
		}
	}
}

func TestSectionOrder_IsTheRenderOrder(t *testing.T) {
	want := []string{"Volumes", "Networks", "Stacks", "Filesets", "Containers"}
	if len(SectionOrder) != len(want) {
		t.Fatalf("SectionOrder = %v, want %v", SectionOrder, want)
	}
	for i := range want {
		if SectionOrder[i] != want[i] {
			t.Errorf("SectionOrder[%d] = %q, want %q", i, SectionOrder[i], want[i])
		}
	}
}

// Every title SectionTitle can return must appear in SectionOrder, or a
// renderer iterating SectionOrder would silently drop that section.
func TestSectionTitle_AllTitlesAppearInOrder(t *testing.T) {
	inOrder := map[string]bool{}
	for _, s := range SectionOrder {
		inOrder[s] = true
	}
	for _, typ := range []ResourceType{
		ResourceVolume, ResourceNetwork, ResourceStack,
		ResourceService, ResourceFileset, ResourceFile, ResourceContainer,
	} {
		if title := SectionTitle(typ); !inOrder[title] {
			t.Errorf("SectionTitle(%v) = %q, absent from SectionOrder %v", typ, title, SectionOrder)
		}
	}
}
