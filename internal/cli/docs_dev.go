//go:build dev

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// registerDocsCmd adds the docs command in dev builds.
func registerDocsCmd(root *cobra.Command) {
	root.AddCommand(newDocsCmd())
}

// newDocsCmd creates a hidden command that generates CLI docs from the Cobra tree.
func newDocsCmd() *cobra.Command {
	var outDir string
	var basePath string

	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate CLI documentation (Markdown tree) with VitePress integration",
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

			// 1) Generate Markdown with VitePress-friendly links + frontmatter
			filePrepender := func(filename string) string {
				// Add VitePress frontmatter with title from filename
				title := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
				title = strings.ReplaceAll(title, "_", " ")
				// Capitalize first letter of each word for better titles
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
				// Cobra gives "subcmd.md" (relative). Convert to VitePress clean link: "/cli/subcmd"
				link = strings.TrimSuffix(link, ".md")
				// Normalize windows slashes just in case
				link = strings.ReplaceAll(link, "\\", "/")
				return basePath + link
			}

			if err := genMarkdownTreeCustomWithHeaderFormat(root, outDir, filePrepender, linkHandler); err != nil {
				return err
			}

			// 2) Build a VitePress sidebar from the Cobra tree
			sidebar := buildSidebar(root, basePath)
			if err := writeSidebar(sidebar, filepath.Join(outDir, "sidebar.mts")); err != nil {
				return err
			}

			fmt.Printf("Generated CLI docs in %s with VitePress integration\n", outDir)
			fmt.Printf("Generated sidebar.mts for VitePress config\n")
			fmt.Printf("Import in .vitepress/config.ts: import cliSidebar from '../cli/sidebar'\n")
			fmt.Printf("Add to config: sidebar: { '%s': cliSidebar }\n", basePath)

			return nil
		},
	}

	cmd.Flags().StringVarP(&outDir, "out", "o", "docs/cli", "Output directory for generated docs")
	cmd.Flags().StringVarP(&basePath, "base", "b", "/cli/", "Base path for VitePress links (e.g., /docs/cli/)")

	return cmd
}

// SidebarItem represents a VitePress sidebar item
type SidebarItem struct {
	Text      string        `json:"text"`
	Link      string        `json:"link,omitempty"`
	Items     []SidebarItem `json:"items,omitempty"`
	Collapsed *bool         `json:"collapsed,omitempty"`
}

// buildSidebar creates a VitePress sidebar structure from the Cobra command tree
func buildSidebar(cmd *cobra.Command, basePath string) []SidebarItem {
	var walk func(c *cobra.Command, path []string, isRoot bool) SidebarItem
	walk = func(c *cobra.Command, path []string, isRoot bool) SidebarItem {
		// Build the filename segment that Cobra generates (commands joined with "_")
		seg := strings.Join(path, "_")

		// Use command name for cleaner navigation
		text := c.Name()

		item := SidebarItem{
			Text: text,
			Link: basePath + seg, // matches filenames cobra generates
		}

		// Get subcommands and sort them alphabetically
		subs := c.Commands()
		// Filter out hidden commands and help commands
		var visibleSubs []*cobra.Command
		for _, sub := range subs {
			if !sub.Hidden && sub.Name() != "help" {
				visibleSubs = append(visibleSubs, sub)
			}
		}

		sort.Slice(visibleSubs, func(i, j int) bool {
			nameI, nameJ := visibleSubs[i].Name(), visibleSubs[j].Name()

			// Put "completion" at the end
			if nameI == "completion" && nameJ != "completion" {
				return false
			}
			if nameJ == "completion" && nameI != "completion" {
				return true
			}

			// Normal alphabetical sorting for everything else
			return nameI < nameJ
		})

		// Recursively build sidebar items for subcommands
		for _, sub := range visibleSubs {
			childPath := append(path, sub.Name())
			item.Items = append(item.Items, walk(sub, childPath, false))
		}

		// Add collapsed: true for items with children, except root and top-level dockform
		if len(item.Items) > 0 && !isRoot {
			collapsed := true
			item.Collapsed = &collapsed
		}

		return item
	}

	// Start with the root command and wrap under "CLI Reference"
	rootPath := []string{cmd.Name()}
	rootItem := walk(cmd, rootPath, true) // Mark as root

	// Wrap everything under an unclickable "CLI Reference" header
	return []SidebarItem{
		{
			Text:  "CLI Reference",
			Items: []SidebarItem{rootItem},
		},
	}
}

// writeSidebar writes the sidebar structure to a TypeScript file for VitePress
func writeSidebar(sidebar []SidebarItem, outputPath string) error {
	var buf bytes.Buffer
	buf.WriteString("// Auto-generated VitePress sidebar for CLI documentation\n")
	buf.WriteString("// Import this in your .vitepress/config.ts file\n\n")
	buf.WriteString("export default ")

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(sidebar); err != nil {
		return err
	}

	return os.WriteFile(outputPath, buf.Bytes(), 0o644)
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
