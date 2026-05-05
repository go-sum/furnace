package creds

import (
	"github.com/google/go-containerregistry/pkg/authn"
)

type tokenKeychain struct {
	token string
}

// TokenKeychain returns an authn.Keychain that authenticates to ghcr.io
// with the given token. Falls back to authn.DefaultKeychain for other registries.
func TokenKeychain(token string) authn.Keychain {
	return tokenKeychain{token: token}
}

func (k tokenKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	if resource.RegistryStr() == "ghcr.io" {
		return authn.FromConfig(authn.AuthConfig{
			Username: "furnace",
			Password: k.token,
		}), nil
	}
	return authn.DefaultKeychain.Resolve(resource)
}
