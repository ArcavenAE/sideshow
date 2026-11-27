package main

import "github.com/ArcavenAE/sideshow/internal/release"

// releaseVerifier is the signature checker used for a signed-release
// install. Nil means "build the cosign one when needed"; tests set a fake.
var releaseVerifier release.Verifier
