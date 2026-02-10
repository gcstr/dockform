package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gcstr/dockform/internal/apperr"
)

func TestNormalize_DefaultsAndFiles(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default":  {},
		},
		Stacks: map[string]Stack{
			"default/web": {
				Root:    "app", // relative, should resolve
				EnvFile: []string{"compose.env"},
			},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	_ = cfg.Contexts["default"] // context exists
	// In new schema, context name IS the Docker context
	app := cfg.Stacks["default/web"]
	wantRoot := filepath.Clean(filepath.Join(base, "app"))
	if app.Root != wantRoot {
		t.Fatalf("root not resolved: want %q got %q", wantRoot, app.Root)
	}
	// When Files empty, default compose.yaml under resolved root
	wantFiles := []string{filepath.Join(wantRoot, "compose.yaml")}
	if !reflect.DeepEqual(app.Files, wantFiles) {
		t.Fatalf("files mismatch:\nwant: %#v\n got: %#v", wantFiles, app.Files)
	}
}

func TestNormalize_InvalidStackKey(t *testing.T) {
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default":  {},
		},
		Stacks: map[string]Stack{"Bad Name": {Root: "/tmp"}},
	}
	if err := cfg.normalizeAndValidate("/base"); err == nil {
		t.Fatalf("expected error for invalid stack key")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestNormalize_MissingIdentifier(t *testing.T) {
	cfg := Config{
		Identifier: "", // Missing identifier
		Contexts: map[string]ContextConfig{
			"default": {},
		},
		Stacks: map[string]Stack{"default/ok": {Root: "/tmp"}},
	}
	if err := cfg.normalizeAndValidate("/base"); err == nil {
		t.Fatalf("expected error for missing identifier")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestNormalize_ContextWithValidHost(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"remote": {Host: "ssh://user@server"},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	if cfg.Contexts["remote"].Host != "ssh://user@server" {
		t.Fatalf("host not preserved: got %q", cfg.Contexts["remote"].Host)
	}
}

func TestNormalize_ContextWithWhitespaceHost(t *testing.T) {
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"remote": {Host: "  "},
		},
	}
	if err := cfg.normalizeAndValidate("/base"); err == nil {
		t.Fatalf("expected error for whitespace-only host")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestNormalize_InlineEnvLastWins(t *testing.T) {
	base := t.TempDir()
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default":  {},
		},
		Stacks: map[string]Stack{
			"default/web": {Root: "app", Environment: &Environment{Inline: []string{"FOO=A", "BAR=2", "BAZ=3"}}},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}
	got := cfg.Stacks["default/web"].EnvInline
	want := []string{"FOO=A", "BAR=2", "BAZ=3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inline env mismatch:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestNormalize_SopsSecretsValidation(t *testing.T) {
	base := t.TempDir()
	// valid case - SOPS secrets at stack level
	cfg := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default":  {},
		},
		Stacks: map[string]Stack{
			"default/web": {Root: "app", SopsSecrets: []string{"secrets.env"}},
		},
	}
	if err := cfg.normalizeAndValidate(base); err != nil {
		t.Fatalf("normalizeAndValidate: %v", err)
	}

	// invalid extension
	cfg2 := Config{
		Identifier: "test",
		Contexts: map[string]ContextConfig{
			"default":  {},
		},
		Stacks: map[string]Stack{
			"default/web": {Root: "app", SopsSecrets: []string{"secrets.txt"}},
		},
	}
	if err := cfg2.normalizeAndValidate(base); err == nil {
		t.Fatalf("expected error for invalid sops extension")
	} else if !apperr.IsKind(err, apperr.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

func TestFindDefaultComposeFile(t *testing.T) {
	// Test selection order: compose.yaml > compose.yml > docker-compose.yaml > docker-compose.yml
	t.Run("prefer_compose_yaml_over_others", func(t *testing.T) {
		dir := t.TempDir()
		composeYaml := filepath.Join(dir, "compose.yaml")
		composeYml := filepath.Join(dir, "compose.yml")
		dcYaml := filepath.Join(dir, "docker-compose.yaml")
		dcYml := filepath.Join(dir, "docker-compose.yml")

		// Create all files
		for _, p := range []string{composeYaml, composeYml, dcYaml, dcYml} {
			if err := os.WriteFile(p, []byte("version: '3'\nservices: {}"), 0644); err != nil {
				t.Fatal(err)
			}
		}

		result := findDefaultComposeFile(dir)
		if result != composeYaml {
			t.Fatalf("expected %s, got %s", composeYaml, result)
		}
	})
	t.Run("prefer_yaml_when_both_exist", func(t *testing.T) {
		dir := t.TempDir()
		yamlFile := filepath.Join(dir, "docker-compose.yaml")
		ymlFile := filepath.Join(dir, "docker-compose.yml")

		// Create both files
		if err := os.WriteFile(yamlFile, []byte("version: '3'\nservices: {}"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ymlFile, []byte("version: '3'\nservices: {}"), 0644); err != nil {
			t.Fatal(err)
		}

		result := findDefaultComposeFile(dir)
		if result != yamlFile {
			t.Fatalf("expected %s, got %s", yamlFile, result)
		}
	})

	// Test finding docker-compose.yml when only yml exists
	t.Run("find_compose_yml_when_only_compose_yml_exists", func(t *testing.T) {
		dir := t.TempDir()
		ymlFile := filepath.Join(dir, "compose.yml")

		if err := os.WriteFile(ymlFile, []byte("version: '3'\nservices: {}"), 0644); err != nil {
			t.Fatal(err)
		}

		result := findDefaultComposeFile(dir)
		if result != ymlFile {
			t.Fatalf("expected %s, got %s", ymlFile, result)
		}
	})

	// Test finding compose.yaml when only compose.yaml exists
	t.Run("find_compose_yaml_when_only_compose_yaml_exists", func(t *testing.T) {
		dir := t.TempDir()
		yamlFile := filepath.Join(dir, "compose.yaml")

		if err := os.WriteFile(yamlFile, []byte("version: '3'\nservices: {}"), 0644); err != nil {
			t.Fatal(err)
		}

		result := findDefaultComposeFile(dir)
		if result != yamlFile {
			t.Fatalf("expected %s, got %s", yamlFile, result)
		}
	})

	// Test default when none exists
	t.Run("default_compose_yaml_when_none_exists", func(t *testing.T) {
		dir := t.TempDir()
		expected := filepath.Join(dir, "compose.yaml")

		result := findDefaultComposeFile(dir)
		if result != expected {
			t.Fatalf("expected %s, got %s", expected, result)
		}
	})
}

func TestNormalize_DefaultComposeFileDetection(t *testing.T) {
	// Test that normalization picks up compose.yaml when available
	t.Run("picks_up_compose_yaml", func(t *testing.T) {
		base := t.TempDir()
		appDir := filepath.Join(base, "app")
		if err := os.MkdirAll(appDir, 0755); err != nil {
			t.Fatal(err)
		}

		yamlFile := filepath.Join(appDir, "compose.yaml")
		if err := os.WriteFile(yamlFile, []byte("version: '3'\nservices: {}"), 0644); err != nil {
			t.Fatal(err)
		}

		cfg := Config{
			Identifier: "test",
		Contexts: map[string]ContextConfig{
				"default":  {},
			},
			Stacks: map[string]Stack{
				"default/web": {Root: "app"}, // No Files specified, should auto-detect
			},
		}

		if err := cfg.normalizeAndValidate(base); err != nil {
			t.Fatalf("normalizeAndValidate: %v", err)
		}

		app := cfg.Stacks["default/web"]
		if len(app.Files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(app.Files))
		}

		if app.Files[0] != yamlFile {
			t.Fatalf("expected %s, got %s", yamlFile, app.Files[0])
		}
	})

	// Test that normalization picks up compose.yml when compose.yaml doesn't exist
	t.Run("picks_up_compose_yml", func(t *testing.T) {
		base := t.TempDir()
		appDir := filepath.Join(base, "app")
		if err := os.MkdirAll(appDir, 0755); err != nil {
			t.Fatal(err)
		}

		ymlFile := filepath.Join(appDir, "compose.yml")
		if err := os.WriteFile(ymlFile, []byte("version: '3'\nservices: {}"), 0644); err != nil {
			t.Fatal(err)
		}

		cfg := Config{
			Identifier: "test",
		Contexts: map[string]ContextConfig{
				"default":  {},
			},
			Stacks: map[string]Stack{
				"default/web": {Root: "app"}, // No Files specified, should auto-detect
			},
		}

		if err := cfg.normalizeAndValidate(base); err != nil {
			t.Fatalf("normalizeAndValidate: %v", err)
		}

		app := cfg.Stacks["default/web"]
		if len(app.Files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(app.Files))
		}

		if app.Files[0] != ymlFile {
			t.Fatalf("expected %s, got %s", ymlFile, app.Files[0])
		}
	})
}

func TestValidateUserOrGroup(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantNumeric bool
		wantErr     bool
	}{
		{"numeric_uid", "1000", true, false},
		{"numeric_zero", "0", true, false},
		{"valid_name_lowercase", "www-data", false, false},
		{"valid_name_underscore", "app_user", false, false},
		{"valid_name_dollar", "user$", false, false},
		{"valid_name_mixed", "apache2", false, false},
		{"empty_string", "", false, true},
		{"spaces_only", "   ", false, true},
		{"invalid_chars", "user@host", false, true},
		{"starts_with_digit", "1user", false, true},
		{"uppercase_name", "NGINX", false, false}, // case-insensitive check
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isNumeric, err := validateUserOrGroup(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUserOrGroup(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil && isNumeric != tt.wantNumeric {
				t.Errorf("validateUserOrGroup(%q) isNumeric = %v, want %v", tt.input, isNumeric, tt.wantNumeric)
			}
		})
	}
}

func TestParseOctalMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint32
		wantErr bool
	}{
		{"mode_644", "644", 0644, false},
		{"mode_0644", "0644", 0644, false},
		{"mode_755", "755", 0755, false},
		{"mode_0755", "0755", 0755, false},
		{"mode_4755", "4755", 04755, false}, // setuid bit
		{"mode_0777", "0777", 0777, false},
		{"empty_string", "", 0, true},
		{"spaces", "   ", 0, true},
		{"too_short", "64", 0, true},
		{"too_long", "07777", 0, true},
		{"invalid_octal", "999", 0, true},
		{"non_numeric", "rw-", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOctalMode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOctalMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil && got != tt.want {
				t.Errorf("parseOctalMode(%q) = 0%o, want 0%o", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateOwnership(t *testing.T) {
	tests := []struct {
		name      string
		ownership *Ownership
		wantErr   bool
	}{
		{
			name:      "nil_ownership",
			ownership: nil,
			wantErr:   false,
		},
		{
			name: "valid_numeric_user_group",
			ownership: &Ownership{
				User:     "1000",
				Group:    "1000",
				FileMode: "0644",
				DirMode:  "0755",
			},
			wantErr: false,
		},
		{
			name: "valid_named_user_group",
			ownership: &Ownership{
				User:     "www-data",
				Group:    "www-data",
				FileMode: "644",
				DirMode:  "755",
			},
			wantErr: false,
		},
		{
			name: "valid_only_user",
			ownership: &Ownership{
				User: "1000",
			},
			wantErr: false,
		},
		{
			name: "valid_only_group",
			ownership: &Ownership{
				Group: "1000",
			},
			wantErr: false,
		},
		{
			name: "valid_only_modes",
			ownership: &Ownership{
				FileMode: "0644",
				DirMode:  "0755",
			},
			wantErr: false,
		},
		{
			name: "invalid_user",
			ownership: &Ownership{
				User: "bad@user",
			},
			wantErr: true,
		},
		{
			name: "invalid_group",
			ownership: &Ownership{
				Group: "bad@group",
			},
			wantErr: true,
		},
		{
			name: "invalid_file_mode",
			ownership: &Ownership{
				FileMode: "999",
			},
			wantErr: true,
		},
		{
			name: "invalid_dir_mode",
			ownership: &Ownership{
				DirMode: "abc",
			},
			wantErr: true,
		},
		{
			name: "preserve_existing_flag",
			ownership: &Ownership{
				User:             "1000",
				PreserveExisting: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &FilesetSpec{Ownership: tt.ownership}
			err := validateOwnership("test-fileset", fs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOwnership() error = %v, wantErr %v", err, tt.wantErr)
			}
			// Verify trimming occurred for valid cases with trimmed ownership
			if err == nil && tt.ownership != nil {
				if tt.ownership.User != "" {
					if strings.TrimSpace(tt.ownership.User) != tt.ownership.User {
						t.Errorf("User should have been trimmed but wasn't")
					}
				}
				if tt.ownership.Group != "" {
					if strings.TrimSpace(tt.ownership.Group) != tt.ownership.Group {
						t.Errorf("Group should have been trimmed but wasn't")
					}
				}
				if tt.ownership.FileMode != "" {
					if strings.TrimSpace(tt.ownership.FileMode) != tt.ownership.FileMode {
						t.Errorf("FileMode should have been trimmed but wasn't")
					}
				}
				if tt.ownership.DirMode != "" {
					if strings.TrimSpace(tt.ownership.DirMode) != tt.ownership.DirMode {
						t.Errorf("DirMode should have been trimmed but wasn't")
					}
				}
			}
		})
	}
}

func TestValidateOwnership_Trimming(t *testing.T) {
	fs := &FilesetSpec{
		Source:       "src",
		TargetVolume: "vol",
		TargetPath:   "/app",
		Ownership: &Ownership{
			User:     " 1000 ",
			Group:    " 1000 ",
			FileMode: " 0644 ",
			DirMode:  " 0755 ",
		},
	}

	err := validateOwnership("test-fileset", fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify values were trimmed
	if fs.Ownership.User != "1000" {
		t.Errorf("User not trimmed: got %q, want %q", fs.Ownership.User, "1000")
	}
	if fs.Ownership.Group != "1000" {
		t.Errorf("Group not trimmed: got %q, want %q", fs.Ownership.Group, "1000")
	}
	if fs.Ownership.FileMode != "0644" {
		t.Errorf("FileMode not trimmed: got %q, want %q", fs.Ownership.FileMode, "0644")
	}
	if fs.Ownership.DirMode != "0755" {
		t.Errorf("DirMode not trimmed: got %q, want %q", fs.Ownership.DirMode, "0755")
	}
}

func TestParseStackKey(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantDaemon string
		wantStack  string
		wantErr    bool
	}{
		{"valid_key", "hetzner/traefik", "hetzner", "traefik", false},
		{"default_daemon", "default/web", "default", "web", false},
		{"nested_path", "prod/apps/web", "prod", "apps/web", false},
		{"missing_daemon", "web", "", "", true},
		{"empty_key", "", "", "", true},
		{"only_slash", "/", "", "", true},
		{"empty_daemon", "/web", "", "", true},
		{"empty_stack", "daemon/", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon, stack, err := ParseStackKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStackKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
				return
			}
			if daemon != tt.wantDaemon {
				t.Errorf("ParseStackKey(%q) daemon = %v, want %v", tt.key, daemon, tt.wantDaemon)
			}
			if stack != tt.wantStack {
				t.Errorf("ParseStackKey(%q) stack = %v, want %v", tt.key, stack, tt.wantStack)
			}
		})
	}
}

func TestGetAllStacks(t *testing.T) {
	cfg := Config{
		Stacks: map[string]Stack{
			"default/web":     {Profiles: []string{"prod"}}, // Augments discovered
			"default/newstack": {Root: "/app/new"},          // Fallback (no discovered)
		},
		DiscoveredStacks: map[string]Stack{
			"default/api":      {Root: "/app/api"},
			"default/web":      {Root: "/discovered/web"}, // Discovery wins for core fields
			"hetzner/frontend": {Root: "/prod/frontend"},
		},
	}

	all := cfg.GetAllStacks()

	if len(all) != 4 {
		t.Fatalf("expected 4 stacks, got %d", len(all))
	}

	// Discovery wins for core fields, explicit adds augmentation
	if web, ok := all["default/web"]; !ok || web.Root != "/discovered/web" {
		t.Errorf("default/web should keep discovered Root=/discovered/web, got %v", web.Root)
	}
	if web, ok := all["default/web"]; !ok || len(web.Profiles) != 1 || web.Profiles[0] != "prod" {
		t.Errorf("default/web should have augmented Profiles=[prod], got %v", web.Profiles)
	}

	// Discovered stacks should be included
	if _, ok := all["default/api"]; !ok {
		t.Errorf("default/api should be in result")
	}
	if _, ok := all["hetzner/frontend"]; !ok {
		t.Errorf("hetzner/frontend should be in result")
	}

	// Explicit-only stack (fallback) should be included
	if newstack, ok := all["default/newstack"]; !ok || newstack.Root != "/app/new" {
		t.Errorf("default/newstack should be from explicit stacks, got %v", all["default/newstack"])
	}
}

func TestGetStacksForDaemon(t *testing.T) {
	cfg := Config{
		Stacks: map[string]Stack{
			"default/web":    {Root: "/app/web"},
			"default/api":    {Root: "/app/api"},
			"hetzner/traefik": {Root: "/prod/traefik"},
		},
	}

	defaultStacks := cfg.GetStacksForContext("default")
	if len(defaultStacks) != 2 {
		t.Fatalf("expected 2 stacks for default context, got %d", len(defaultStacks))
	}

	hetznerStacks := cfg.GetStacksForContext("hetzner")
	if len(hetznerStacks) != 1 {
		t.Fatalf("expected 1 stack for hetzner context, got %d", len(hetznerStacks))
	}

	nonexistentStacks := cfg.GetStacksForContext("nonexistent")
	if len(nonexistentStacks) != 0 {
		t.Fatalf("expected 0 stacks for nonexistent context, got %d", len(nonexistentStacks))
	}
}

func TestGetAllSopsSecrets(t *testing.T) {
	cfg := Config{
		Stacks: map[string]Stack{
			"default/web": {SopsSecrets: []string{"web.env", "shared.env"}},
			"default/api": {SopsSecrets: []string{"api.env", "shared.env"}}, // shared.env should be deduped
		},
	}

	secrets := cfg.GetAllSopsSecrets()

	// Should have 3 unique secrets
	if len(secrets) != 3 {
		t.Fatalf("expected 3 unique secrets, got %d: %v", len(secrets), secrets)
	}

	// Check all expected secrets are present
	found := make(map[string]bool)
	for _, s := range secrets {
		found[s] = true
	}
	if !found["web.env"] || !found["api.env"] || !found["shared.env"] {
		t.Errorf("missing expected secrets: %v", secrets)
	}
}
