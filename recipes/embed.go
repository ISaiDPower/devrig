// Package recipes holds the package recipes built into rig.
package recipes

import "embed"

// FS holds <name>/recipe.toml and <name>/versions.json for every package.
//
//go:embed */recipe.toml */versions.json
var FS embed.FS
