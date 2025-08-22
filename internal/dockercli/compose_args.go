package dockercli

import "path/filepath"

func (c *Client) composeBaseArgs(files, profiles, envFiles []string, projectName string) []string {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", filepath.Clean(f))
	}
	if projectName != "" {
		args = append(args, "-p", projectName)
	}
	for _, e := range envFiles {
		args = append(args, "--env-file", filepath.Clean(e))
	}
	for _, p := range profiles {
		args = append(args, "--profile", p)
	}
	return args
}
