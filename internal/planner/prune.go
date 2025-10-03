package planner

import (
	"context"
	"path/filepath"

	"github.com/gcstr/dockform/internal/apperr"
	"github.com/gcstr/dockform/internal/manifest"
	"github.com/gcstr/dockform/internal/secrets"
)

// Prune removes unmanaged resources labeled with the identifier.
// It deletes volumes, networks, and containers that are labeled but not present in cfg.
func (p *Planner) Prune(ctx context.Context, cfg manifest.Config) error {
	if p.docker == nil {
		return apperr.New("planner.Prune", apperr.Precondition, "docker client not configured")
	}
	// Desired services set across all applications
	desiredServices := map[string]struct{}{}
	for _, app := range cfg.Applications {
		inline := append([]string(nil), app.EnvInline...)
		ageKeyFile := ""
		pgpDir := ""
		pgpAgent := false
		pgpMode := ""
		pgpPass := ""
		if cfg.Sops != nil && cfg.Sops.Age != nil {
			ageKeyFile = cfg.Sops.Age.KeyFile
		}
		if cfg.Sops != nil && cfg.Sops.Pgp != nil {
			pgpDir = cfg.Sops.Pgp.KeyringDir
			pgpAgent = cfg.Sops.Pgp.UseAgent
			pgpMode = cfg.Sops.Pgp.PinentryMode
			pgpPass = cfg.Sops.Pgp.Passphrase
		}
		for _, pth0 := range app.SopsSecrets {
			pth := pth0
			if pth != "" && !filepath.IsAbs(pth) {
				pth = filepath.Join(app.Root, pth)
			}
			if pairs, err := secrets.DecryptAndParse(ctx, pth, secrets.SopsOptions{AgeKeyFile: ageKeyFile, PgpKeyringDir: pgpDir, PgpUseAgent: pgpAgent, PgpPinentryMode: pgpMode, PgpPassphrase: pgpPass}); err == nil {
				inline = append(inline, pairs...)
			}
		}
		if doc, err := p.docker.ComposeConfigFull(ctx, app.Root, app.Files, app.Profiles, app.EnvFile, inline); err == nil {
			for s := range doc.Services {
				desiredServices[s] = struct{}{}
			}
		}
	}
	// Remove labeled containers not in desired set
	if all, err := p.docker.ListComposeContainersAll(ctx); err == nil {
		for _, it := range all {
			if _, want := desiredServices[it.Service]; !want {
				_ = p.docker.RemoveContainer(ctx, it.Name, true)
			}
		}
	}
	// Remove labeled volumes not needed by any fileset
	desiredVolumes := map[string]struct{}{}
	for _, fileset := range cfg.Filesets {
		desiredVolumes[fileset.TargetVolume] = struct{}{}
	}
	if vols, err := p.docker.ListVolumes(ctx); err == nil {
		for _, v := range vols {
			if _, want := desiredVolumes[v]; !want {
				_ = p.docker.RemoveVolume(ctx, v)
			}
		}
	}
	// Remove labeled networks not in cfg
	if nets, err := p.docker.ListNetworks(ctx); err == nil {
		for _, n := range nets {
			if _, want := cfg.Networks[n]; !want {
				_ = p.docker.RemoveNetwork(ctx, n)
			}
		}
	}
	return nil
}
