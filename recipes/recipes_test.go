package recipes

import (
	"testing"

	"github.com/ISaiDPower/devrig/internal/registry"
)

func TestBuiltinRecipesLoad(t *testing.T) {
	r, err := registry.Load(FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Packages()) == 0 {
		t.Fatal("no built-in recipes")
	}

	php, err := r.Package("php")
	if err != nil {
		t.Fatal(err)
	}
	if len(php.Releases("windows/amd64")) == 0 {
		t.Error("php has no windows/amd64 releases")
	}
}
