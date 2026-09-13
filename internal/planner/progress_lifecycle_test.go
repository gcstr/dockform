package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

// assertValidLifecycle enforces the ProgressReporter contract documented on
// ui_hooks.go: per ResourceRef, at most one Start, no Detail/Finish/Fail
// before that ref's Start, and exactly one terminal event (Finish or Fail)
// for any ref that was started. Failures name the offending ref and event so
// a future regression is diagnosable without reading this helper.
func assertValidLifecycle(t *testing.T, events []recordedEvent) {
	t.Helper()

	startedRefs := map[ResourceRef]bool{}
	terminalKind := map[ResourceRef]string{}

	for i, e := range events {
		switch e.Kind {
		case "seed":
			continue
		case "start":
			if startedRefs[e.Ref] {
				t.Errorf("event #%d: ref %+v received a second Start (verb %q); only one Start is allowed per ref", i, e.Ref, e.Text)
				continue
			}
			startedRefs[e.Ref] = true
		case "detail":
			if !startedRefs[e.Ref] {
				t.Errorf("event #%d: Detail(%q) for ref %+v arrived before that ref's Start", i, e.Text, e.Ref)
			}
		case "finish", "fail":
			if !startedRefs[e.Ref] {
				t.Errorf("event #%d: %s(%q) for ref %+v arrived before that ref's Start", i, e.Kind, e.Text, e.Ref)
			}
			if prev, ok := terminalKind[e.Ref]; ok {
				t.Errorf("event #%d: ref %+v received a second terminal event (%s) after already reaching %s", i, e.Ref, e.Kind, prev)
				continue
			}
			terminalKind[e.Ref] = e.Kind
		default:
			t.Errorf("event #%d: ref %+v has unrecognized event kind %q", i, e.Ref, e.Kind)
		}
	}

	for ref := range startedRefs {
		if _, ok := terminalKind[ref]; !ok {
			t.Errorf("ref %+v was started but never reached a terminal event (Finish or Fail)", ref)
		}
	}
}

// TestProgressLifecycle_VolumeCreate exercises the volume path end to end
// through a real ResourceManager + mock docker client, and asserts the exact
// Start/Finish sequence in addition to the general contract.
func TestProgressLifecycle_VolumeCreate(t *testing.T) {
	mockDocker := newMockDocker()
	rec := &recordingReporter{}
	rm := NewResourceManagerWithClient(mockDocker, rec)

	cfg := manifest.Config{
		Identifier: "test-id",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"default/web/data": {TargetVolume: "app-data", Context: "default"},
		},
	}

	if _, err := rm.EnsureVolumesExistForContext(context.Background(), cfg, "default", map[string]string{"test": "label"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ref := ResourceRef{Context: "default", Type: ResourceVolume, Name: "app-data"}

	var seq []recordedEvent
	for _, e := range rec.events {
		if e.Ref == ref {
			seq = append(seq, e)
		}
	}
	if len(seq) != 2 || seq[0].Kind != "start" || seq[0].Text != "creating" || seq[1].Kind != "finish" || seq[1].Text != "created" {
		t.Fatalf("event sequence for ref %+v = %+v, want [start:creating finish:created]", ref, seq)
	}

	assertValidLifecycle(t, rec.events)
}

// TestProgressLifecycle_FilesetDeleteOnly is the exact case behind the
// reviewer finding: a fileset whose diff contains only deletions (the user
// deleted a local file). Before the fix, syncFilesetFiles returned early
// before any Start fired, so this ref's Detail/Finish events arrived on a
// line stuck in StatePending — assertValidLifecycle would have failed here.
func TestProgressLifecycle_FilesetDeleteOnly(t *testing.T) {
	mockDocker := newMockDocker()
	rec := &recordingReporter{}
	fm := NewFilesetManagerWithClient(mockDocker, rec)

	fileset := manifest.FilesetSpec{
		SourceAbs:    "/tmp/progress-lifecycle-test-source",
		TargetVolume: "app-data",
		TargetPath:   "/data",
		Context:      "default",
	}

	cfg := manifest.Config{
		Identifier: "test-id",
		Contexts: map[string]manifest.ContextConfig{
			"default": {},
		},
		DiscoveredFilesets: map[string]manifest.FilesetSpec{
			"myfileset": fileset,
		},
	}

	// Pre-computed execution context, as BuildPlan would hand to Apply: a diff
	// with only ToDelete entries, and mismatched tree hashes so the "nothing
	// to do" skip branch is not taken.
	execCtx := NewContextExecutionContext("default", "test-id")
	execCtx.Filesets["myfileset"] = &FilesetExecutionData{
		LocalIndex:  filesets.Index{TreeHash: "local-hash"},
		RemoteIndex: filesets.Index{TreeHash: "remote-hash"},
		Diff:        filesets.Diff{ToDelete: []string{"old-file.txt"}},
	}

	if _, err := fm.SyncFilesetsForContext(context.Background(), cfg, "default", map[string]struct{}{}, execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ref := ResourceRef{Context: "default", Type: ResourceFileset, Name: "myfileset"}
	got := rec.kinds(ref)
	if len(got) == 0 {
		t.Fatalf("no events recorded for delete-only fileset ref %+v", ref)
	}
	if got[0] != "start" {
		t.Errorf("first event for ref %+v = %q, want %q", ref, got[0], "start")
	}
	if last := got[len(got)-1]; last != "finish" {
		t.Errorf("last event for ref %+v = %q, want %q", ref, last, "finish")
	}

	assertValidLifecycle(t, rec.events)
}
