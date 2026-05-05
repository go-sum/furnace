package creds

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
)

func TestCreateDockerConfigDir(t *testing.T) {
	dockerDir, err := CreateDockerConfigDir("ghp_testtoken")
	if err != nil {
		t.Fatalf("CreateDockerConfigDir: %v", err)
	}
	t.Cleanup(func() {
		_ = RemoveDockerConfigDir(dockerDir)
	})

	data, err := os.ReadFile(dockerDir + "/config.json")
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}

	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config.json: %v", err)
	}

	entry, ok := cfg.Auths["ghcr.io"]
	if !ok {
		t.Fatal("expected ghcr.io key in auths")
	}
	if entry.Auth == "" {
		t.Fatal("expected non-empty auth value")
	}

	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	if string(decoded) != "furnace:ghp_testtoken" {
		t.Fatalf("decoded auth: got %q, want %q", decoded, "furnace:ghp_testtoken")
	}
}

func TestRemoveDockerConfigDir(t *testing.T) {
	t.Run("removes existing directory", func(t *testing.T) {
		dockerDir, err := CreateDockerConfigDir("ghp_testtoken")
		if err != nil {
			t.Fatalf("CreateDockerConfigDir: %v", err)
		}
		if err := RemoveDockerConfigDir(dockerDir); err != nil {
			t.Fatalf("RemoveDockerConfigDir: %v", err)
		}
		if _, err := os.Stat(dockerDir); !os.IsNotExist(err) {
			t.Fatalf("expected docker config dir to be removed, stat err: %v", err)
		}
	})

	t.Run("no error when directory does not exist", func(t *testing.T) {
		if err := RemoveDockerConfigDir(t.TempDir() + "/missing"); err != nil {
			t.Fatalf("RemoveDockerConfigDir on missing dir: %v", err)
		}
	})
}

func TestLoadFromCredentialsDir_Missing(t *testing.T) {
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	got, err := LoadFromCredentialsDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestLoadFromCredentialsDir_Present(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/registry-token", []byte("mytoken\n"), 0600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	got, err := LoadFromCredentialsDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "mytoken" {
		t.Fatalf("got %q, want %q", got, "mytoken")
	}
}

type fakeResource struct{ host string }

func (r fakeResource) RegistryStr() string { return r.host }
func (r fakeResource) String() string      { return r.host }

func TestTokenKeychain_GhcrIO(t *testing.T) {
	kc := TokenKeychain("mytoken")
	auth, err := kc.Resolve(fakeResource{host: "ghcr.io"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got, err := auth.Authorization()
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}

	expected := authn.FromConfig(authn.AuthConfig{Username: "furnace", Password: "mytoken"})
	want, err := expected.Authorization()
	if err != nil {
		t.Fatalf("expected Authorization: %v", err)
	}

	if got.Username != want.Username || got.Password != want.Password {
		t.Fatalf("auth mismatch: got {%s, %s}, want {%s, %s}", got.Username, got.Password, want.Username, want.Password)
	}
}

func TestTokenKeychain_OtherRegistry(t *testing.T) {
	kc := TokenKeychain("mytoken")
	_, err := kc.Resolve(fakeResource{host: "docker.io"})
	if err != nil {
		t.Fatalf("Resolve for docker.io should not error: %v", err)
	}
}

func TestTokenKeychain_SubstringNoMatch(t *testing.T) {
	kc := TokenKeychain("mytoken")
	for _, host := range []string{"not-ghcr.io", "ghcr.io.evil.com", "myghcr.io"} {
		auth, err := kc.Resolve(fakeResource{host: host})
		if err != nil {
			t.Fatalf("Resolve(%q): unexpected error: %v", host, err)
		}
		creds, err := auth.Authorization()
		if err != nil {
			// DefaultKeychain may error for unknown registries — not a failure
			continue
		}
		if creds.Password == "mytoken" {
			t.Errorf("host %q received furnace token but should not", host)
		}
	}
}
