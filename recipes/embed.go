// Package recipes holds the package recipes built into rig.
package recipes

import "embed"

// FS holds one directory per package with its recipe.toml, versions.json
// and any template files the recipe refers to.
//
//go:embed */*
var FS embed.FS
