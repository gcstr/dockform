//go:build dev

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// registerDocsCmd adds the docs command in dev builds.
func registerDocsCmd(root *cobra.Command) {
	root.AddCommand(newDocsCmd())
}

// newDocsCmd creates a hidden command that generates the CLI docs (a Markdown
// tree) consumed by the Zensical documentation site. Navigation is defined in the
// site's zensical.toml, so no sidebar is emitted here.
func newDocsCmd() *cobra.Command {
	var outDir string
	var basePath string

	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate CLI documentation (Markdown tree) from the Cobra tree",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if outDir == "" {
				outDir = "docs/cli"
			}
			if basePath == "" {
				basePath = "/docs/cli/"
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}

			root := cmd.Root()
			root.DisableAutoGenTag = true

			// Generate Markdown with frontmatter and clean inter-command links.
			filePrepender := func(filename string) string {
				// Title from filename, e.g. "dockform_volume_snapshot" -> "Dockform Volume Snapshot".
				title := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
				title = strings.ReplaceAll(title, "_", " ")
				words := strings.Split(title, " ")
				for i, word := range words {
					if len(word) > 0 {
						words[i] = strings.ToUpper(word[:1]) + word[1:]
					}
				}
				title = strings.Join(words, " ")

				return fmt.Sprintf("---\ntitle: %s\n---\n\n", title)
			}

			linkHandler := func(link string) string {
				// Cobra gives "subcmd.md" (relative). Convert to a clean link: "/cli/subcmd".
				link = strings.TrimSuffix(link, ".md")
				link = strings.ReplaceAll(link, "\\", "/")
				return basePath + link
			}

			if err := genMarkdownTreeCustomWithHeaderFormat(root, outDir, filePrepender, linkHandler); err != nil {
				return err
			}

			fmt.Printf("Generated CLI docs in %s\n", outDir)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "docs/cli", "Output directory for generated docs")
	cmd.Flags().StringVarP(&basePath, "base", "b", "/cli/", "Base path for inter-command links")

	return cmd
}

// genMarkdownTreeCustomWithHeaderFormat generates markdown docs with custom header formatting
func genMarkdownTreeCustomWithHeaderFormat(cmd *cobra.Command, dir string, filePrepender, linkHandler func(string) string) error {
	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		if err := genMarkdownTreeCustomWithHeaderFormat(c, dir, filePrepender, linkHandler); err != nil {
			return err
		}
	}

	basename := strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".md"
	filename := filepath.Join(dir, basename)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(filePrepender(filename)); err != nil {
		return err
	}
	if err := genMarkdownCustomWithHeaderFormat(cmd, f, linkHandler); err != nil {
		return err
	}
	return nil
}

// genMarkdownCustomWithHeaderFormat generates markdown for a command with custom header formatting
func genMarkdownCustomWithHeaderFormat(cmd *cobra.Command, w *os.File, linkHandler func(string) string) error {
	// Generate the content using the standard function
	buf := new(bytes.Buffer)
	if err := doc.GenMarkdownCustom(cmd, buf, linkHandler); err != nil {
		return err
	}

	// Replace the h2 header with h1 inline code format
	content := buf.String()
	commandPath := cmd.CommandPath()

	// Replace ## command with # `command`
	oldHeader := fmt.Sprintf("## %s", commandPath)
	newHeader := fmt.Sprintf("# `%s`", commandPath)
	content = strings.Replace(content, oldHeader, newHeader, 1)

	_, err := w.WriteString(content)
	return err
}
