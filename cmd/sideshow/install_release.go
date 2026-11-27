package main

import "github.com/ArcavenAE/sideshow/internal/release"

// releaseVerifier is the signature checker used for a signed-release
// install. Nil means "build the cosign one when needed"; tests set a fake.
var releaseVerifier release.Verifier

// installVerifier returns the verifier for a signed-release install.
func installVerifier() (release.Verifier, error) {
	if releaseVerifier != nil {
		return releaseVerifier, nil
	}
	return release.NewCosign("")
}
