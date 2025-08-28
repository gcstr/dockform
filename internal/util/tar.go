package util

import (
	"archive/tar"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
