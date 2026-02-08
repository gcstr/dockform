package planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/filesets"
	"github.com/gcstr/dockform/internal/manifest"
)

func TestBuildOwnershipScript_RejectsUnsafeTargetPath(t *testing.T) {
	ownership := &manifest.Ownership{User: "1000"}
	if _, err := buildOwnershipScript("/", ownership, filesets.Diff{}); err == nil {
		t.Fatalf("expected unsafe target path error")
	}
	if _, err := buildOwnershipScript(".", ownership, filesets.Diff{}); err == nil {
		t.Fatalf("expected unsafe relative target path error")
	}
}

func TestBuildOwnershipScript_RecursiveModeIncludesExpectedCommands(t *testing.T) {
	ownership := &manifest.Ownership{
		User:     "1000",
		Group:    "1001",
		FileMode: "0644",
		DirMode:  "0755",
	}
	script, err := buildOwnershipScript("/app", ownership, filesets.Diff{})
	if err != nil {
		t.Fatalf("build script: %v", err)
	}
	if !strings.Contains(script, "UID_VAL='1000'") || !strings.Contains(script, "GID_VAL='1001'") {
		t.Fatalf("expected numeric uid/gid assignments, got:\n%s", script)
	}
	if !strings.Contains(script, "find '/app' -type d -exec chmod '0755'") {
		t.Fatalf("expected recursive dir chmod command, got:\n%s", script)
	}
	if !strings.Contains(script, "find '/app' -type f -exec chmod '0644'") {
		t.Fatalf("expected recursive file chmod command, got:\n%s", script)
	}
	if !strings.Contains(script, "chown -R \"$UID_VAL:$GID_VAL\" '/app'") {
		t.Fatalf("expected recursive chown command, got:\n%s", script)
	}
}

func TestBuildOwnershipScript_PreserveExistingTargetsOnlyChangedPaths(t *testing.T) {
	ownership := &manifest.Ownership{
		User:             "1000",
		Group:            "1000",
		FileMode:         "0644",
		DirMode:          "0755",
		PreserveExisting: true,
	}
	diff := filesets.Diff{
		ToCreate: []filesets.FileEntry{{Path: "a.txt"}, {Path: "nested/b.txt"}},
		ToUpdate: []filesets.FileEntry{{Path: "c.txt"}},
	}
	script, err := buildOwnershipScript("/app", ownership, diff)
	if err != nil {
		t.Fatalf("build script: %v", err)
	}
	if strings.Contains(script, "chown -R") {
		t.Fatalf("preserve_existing should not use recursive chown, got:\n%s", script)
	}
	if !strings.Contains(script, "[ -f '/app/a.txt' ] && chmod '0644' '/app/a.txt'") {
		t.Fatalf("expected chmod for created file, got:\n%s", script)
	}
	if !strings.Contains(script, "[ -f '/app/nested/b.txt' ] && chmod '0644' '/app/nested/b.txt'") {
		t.Fatalf("expected chmod for nested file, got:\n%s", script)
	}
	if !strings.Contains(script, "chown \"$UID_VAL:$GID_VAL\" '/app/c.txt'") {
		t.Fatalf("expected chown for updated file path, got:\n%s", script)
	}
}

func TestApplyOwnership_SkipsWhenOwnershipMissing(t *testing.T) {
	mockDocker := newMockDocker()
	fm := NewFilesetManager(mockDocker, nil)
	err := fm.applyOwnership(context.Background(), "assets", manifest.FilesetSpec{
		TargetVolume: "data",
		TargetPath:   "/app",
		Ownership:    nil,
	}, filesets.Diff{})
	if err != nil {
		t.Fatalf("expected nil when ownership is absent, got: %v", err)
	}
	if mockDocker.runVolumeScriptRuns != 0 {
		t.Fatalf("did not expect volume script execution, got runs=%d", mockDocker.runVolumeScriptRuns)
	}
}

func TestApplyOwnership_ReturnsErrorWhenScriptBuildFails(t *testing.T) {
	mockDocker := newMockDocker()
	fm := NewFilesetManager(mockDocker, nil)
	err := fm.applyOwnership(context.Background(), "assets", manifest.FilesetSpec{
		TargetVolume: "data",
		TargetPath:   "/",
		Ownership:    &manifest.Ownership{User: "1000"},
	}, filesets.Diff{})
	if err == nil {
		t.Fatalf("expected ownership script build error")
	}
	if !apperr.IsKind(err, apperr.Internal) {
		t.Fatalf("expected internal error kind, got: %v", err)
	}
	if mockDocker.runVolumeScriptRuns != 0 {
		t.Fatalf("did not expect script execution when script build fails, got runs=%d", mockDocker.runVolumeScriptRuns)
	}
}

func TestApplyOwnership_ReturnsErrorWhenScriptExecutionFails(t *testing.T) {
	mockDocker := newMockDocker()
	mockDocker.runVolumeScriptError = errors.New("script failed")
	fm := NewFilesetManager(mockDocker, nil)
	err := fm.applyOwnership(context.Background(), "assets", manifest.FilesetSpec{
		TargetVolume: "data",
		TargetPath:   "/app",
		Ownership:    &manifest.Ownership{User: "1000"},
	}, filesets.Diff{})
	if err == nil {
		t.Fatalf("expected ownership execution error")
	}
	if !apperr.IsKind(err, apperr.External) {
		t.Fatalf("expected external error kind, got: %v", err)
	}
	if mockDocker.runVolumeScriptRuns != 1 {
		t.Fatalf("expected one volume script run, got runs=%d", mockDocker.runVolumeScriptRuns)
	}
}
