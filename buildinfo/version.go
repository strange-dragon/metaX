// Package buildinfo exposes the server build version to the metaX entrypoint.
// Version values are injected at build time through -ldflags, and the HTTP
// handlers read the same values from internal/version.
package buildinfo

import internal "github.com/strange-dragon/metaX/internal/version"

// BuildString returns the same version string used by GET /.
func BuildString() string {
	return internal.BuildString()
}
