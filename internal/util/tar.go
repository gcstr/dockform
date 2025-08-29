package util

import (
	"archive/tar"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// TarDirectoryToWriter walks localDir and writes a tar stream to w.
// Each entry path in the archive is prefixed with targetPrefix when non-empty and not ".".
// Extract with `tar -xpf - -C <dest>`.
func TarDirectoryToWriter(localDir string, targetPrefix string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()
	// Normalize inputs
	localDir = filepath.Clean(localDir)
	usePrefix := targetPrefix != "" && targetPrefix != "."
	return filepath.WalkDir(localDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, p)
		if err != nil {
			return err
		}
		// Skip the root directory header; not necessary for extraction
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if usePrefix {
			name = path.Join(targetPrefix, name)
		}
		mode := int64(info.Mode().Perm())
		hdr := &tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     0,
			ModTime:  info.ModTime(),
			Typeflag: tar.TypeReg,
		}
		if info.IsDir() {
			// Ensure directory header ends with slash
			if name != "/" && name != "" && name[len(name)-1] != '/' {
				hdr.Name = name + "/"
			}
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = target
			hdr.Size = 0
			return tw.WriteHeader(hdr)
		}
		if !info.Mode().IsRegular() {
			// Skip special files
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		hdr.Size = info.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	})
}

// TarFilesToWriter writes a tar stream containing only the provided relative file paths from localRoot.
// - files must be relative to localRoot and use OS path separators. They will be normalized to forward slashes in the archive.
// - directories will be created implicitly for files; directory headers are included as needed.
// - symlinks are ignored; non-regular special files are skipped.
func TarFilesToWriter(localRoot string, files []string, w io.Writer) error {
	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()
	localRoot = filepath.Clean(localRoot)

	// Map to ensure we emit needed directory headers once
	emittedDirs := map[string]bool{}

	// Helper to emit a directory header (name must end with '/'), creating parents first
	var emitDir func(string, int64) error
	emitDir = func(dir string, mode int64) error {
		if dir == "." || dir == "" || dir == "/" {
			return nil
		}
		// Normalize to forward slashes and ensure trailing slash
		name := filepath.ToSlash(dir)
		if name[len(name)-1] != '/' {
			name += "/"
		}
		// Recurse to parent
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent != "." && parent != dir && !emittedDirs[parent] {
			if err := emitDir(parent, 0o755); err != nil {
				return err
			}
		}
		if emittedDirs[name] {
			return nil
		}
		hdr := &tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     0,
			Typeflag: tar.TypeDir,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		emittedDirs[name] = true
		return nil
	}

	for _, rel := range files {
		if rel == "" {
			continue
		}
		// Clean and ensure no path escape
		cleanRel := filepath.Clean(rel)
		if strings.HasPrefix(cleanRel, "..") {
			continue
		}
		abs := filepath.Join(localRoot, cleanRel)
		info, err := os.Lstat(abs)
		if err != nil {
			return err
		}
		// Ensure parent directories are emitted
		if err := emitDir(filepath.Dir(cleanRel), 0o755); err != nil {
			return err
		}
		name := filepath.ToSlash(cleanRel)
		mode := int64(info.Mode().Perm())
		hdr := &tar.Header{Name: name, Mode: mode, ModTime: info.ModTime()}
		if info.IsDir() {
			// Ensure trailing slash
			if name == "" || name == "/" || name[len(name)-1] != '/' {
				name += "/"
			}
			hdr.Name = name
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}
		// Ignore symlinks entirely for assets
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.Mode().IsRegular() {
			// Skip non-regular files
			continue
		}
		f, err := os.Open(abs)
		if err != nil {
			return err
		}
		hdr.Typeflag = tar.TypeReg
		hdr.Size = info.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			_ = f.Close()
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}
	return nil
}
