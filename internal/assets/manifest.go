package assets

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/gcstr/dockform/internal/util"
)

const ManifestFileName = ".dockform-manifest.json"

type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

type Manifest struct {
	Version   string      `json:"version"`
	Target    string      `json:"target_path"`
	CreatedAt string      `json:"created_at"`
	Exclude   []string    `json:"exclude"`
	UID       int         `json:"uid"`
	GID       int         `json:"gid"`
	Files     []FileEntry `json:"files"`
	TreeHash  string      `json:"tree_hash"`
}

func BuildLocalManifest(sourceDir string, targetPath string, excludes []string) (Manifest, error) {
	m := Manifest{
		Version:   "v1",
		Target:    targetPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Exclude:   nil,
		UID:       0,
		GID:       0,
	}
	src := filepath.Clean(sourceDir)

	// Normalize and freeze exclude patterns for determinism
	normEx := normalizeExcludePatterns(excludes)
	// Persist effective excludes into the manifest
	m.Exclude = append(m.Exclude, normEx...)
	files := []FileEntry{}

	// Exclude matcher using doublestar against slash-normalized relative paths
	isExcluded := func(relSlash string, isDir bool) bool {
		for _, pat := range normEx {
			// Directory patterns already expanded to /** in normalization
			match, _ := doublestar.PathMatch(pat, relSlash)
			if match {
				return true
			}
		}
		return false
	}

	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Guard against path traversal escapes
		cleanRel := filepath.Clean(rel)
		if strings.HasPrefix(cleanRel, "..") {
			return fs.ErrInvalid
		}
		relSlash := filepath.ToSlash(cleanRel)
		if d.IsDir() {
			if isExcluded(relSlash, true) {
				return filepath.SkipDir
			}
			return nil
		}
		// Ignore symlinks entirely for assets
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if isExcluded(relSlash, false) {
			return nil
		}
		sum, err := util.Sha256FileHex(p)
		if err != nil {
			return err
		}
		files = append(files, FileEntry{Path: relSlash, Size: info.Size(), Sha256: sum})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	m.Files = files
	// Build tree hash: path + "\x00" + size + "\x00" + sha256 + "\n"
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f.Path)
		b.WriteByte('\x00')
		b.WriteString(strconv.FormatInt(f.Size, 10))
		b.WriteByte('\x00')
		b.WriteString(f.Sha256)
		b.WriteByte('\n')
	}
	m.TreeHash = util.Sha256StringHex(b.String())
	return m, nil
}

// normalizeExcludePatterns returns a deterministic slice of patterns normalized to gitignore-like semantics:
// - trim spaces and skip empty
// - convert OS-specific separators to forward slashes
// - if a pattern ends with '/', expand to pattern + "**" to exclude dir and all contents
// - ensure order is stable by sorting unique patterns
func normalizeExcludePatterns(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	uniq := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		// convert to slash-normalized pattern
		p = filepath.ToSlash(p)
		// directory pattern ending with '/'
		if strings.HasSuffix(p, "/") {
			p = p + "**"
		}
		if _, ok := uniq[p]; ok {
			continue
		}
		uniq[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) > 1 {
		sort.Strings(out)
	}
	return out
}

func ParseManifestJSON(s string) (Manifest, error) {
	if strings.TrimSpace(s) == "" {
		return Manifest{Version: "v1", Files: nil}, nil
	}
	var m Manifest
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) ToJSON() (string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type Diff struct {
	ToCreate []FileEntry
	ToUpdate []FileEntry
	ToDelete []string
}

func DiffManifests(local, remote Manifest) Diff {
	if local.TreeHash != "" && local.TreeHash == remote.TreeHash {
		return Diff{}
	}
	rIndex := map[string]FileEntry{}
	for _, rf := range remote.Files {
		rIndex[rf.Path] = rf
	}
	lIndex := map[string]FileEntry{}
	for _, lf := range local.Files {
		lIndex[lf.Path] = lf
	}
	d := Diff{}
	for _, lf := range local.Files {
		if rf, ok := rIndex[lf.Path]; !ok {
			d.ToCreate = append(d.ToCreate, lf)
		} else if rf.Sha256 != lf.Sha256 || rf.Size != lf.Size {
			d.ToUpdate = append(d.ToUpdate, lf)
		}
	}
	for _, rf := range remote.Files {
		if _, ok := lIndex[rf.Path]; !ok {
			d.ToDelete = append(d.ToDelete, rf.Path)
		}
	}
	sort.Slice(d.ToCreate, func(i, j int) bool { return d.ToCreate[i].Path < d.ToCreate[j].Path })
	sort.Slice(d.ToUpdate, func(i, j int) bool { return d.ToUpdate[i].Path < d.ToUpdate[j].Path })
	sort.Strings(d.ToDelete)
	return d
}
