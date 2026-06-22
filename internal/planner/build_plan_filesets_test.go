package planner

import (
	"context"
	"testing"

	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestBuildFilesetResources_BatchesRemoteIndexReads(t *testing.T) {
	m := newMockDocker()
	m.volumes = []string{"vol1", "vol2"}

	// Build a local index for an empty source dir so its tree hash is deterministic,
	// then store a matching JSON for vol1 in m.volumeFiles.
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	specs := map[string]manifest.FilesetSpec{
		"ctx/s/vol1": {SourceAbs: dir1, TargetPath: "/data", TargetVolume: "vol1"},
		"ctx/s/vol2": {SourceAbs: dir2, TargetPath: "/data", TargetVolume: "vol2"},
	}

	local1, err := filesets.BuildLocalIndex(dir1, "/data", nil)
	if err != nil {
		t.Fatalf("local index: %v", err)
	}
	idxJSON, err := local1.ToJSON() // same serializer the apply path uses (filesets.Index.ToJSON)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	m.volumeFiles["vol1"] = idxJSON

	existing := map[string]struct{}{"vol1": {}, "vol2": {}}
	plan := &ResourcePlan{Filesets: map[string][]Resource{}}
	execCtx := &ContextExecutionContext{Filesets: map[string]*FilesetExecutionData{}}

	p := &Planner{}
	if err := p.buildFilesetResourcesForContext(context.Background(), specs, existing, m, plan, execCtx); err != nil {
		t.Fatalf("buildFilesetResourcesForContext: %v", err)
	}

	if m.readIndexBatchCalls != 1 {
		t.Fatalf("expected exactly 1 batched index read, got %d", m.readIndexBatchCalls)
	}
	// vol1 matched -> no-op; vol2 empty remote -> changes (or no-op if dir2 empty too).
	if len(plan.Filesets["ctx/s/vol1"]) == 0 {
		t.Fatalf("expected a resource entry for vol1")
	}
}
