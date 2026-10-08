package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtends(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "base.yaml"), []byte("name: base\nendpoint: {silence_ms: 400, early_silence_ms: 200}\nnlu: {fast_path: true}\n"), 0o644)
	os.WriteFile(filepath.Join(d, "abl.yaml"), []byte("extends: base.yaml\nname: abl\nendpoint: {silence_ms: 800}\n"), 0o644)
	c, err := Load(filepath.Join(d, "abl.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "abl" || c.Endpoint.SilenceMs != 800 || c.Endpoint.EarlySilenceMs != 200 || !c.NLU.FastPath {
		t.Fatalf("%+v", c)
	}
	if _, err := Load("../../config/default.yaml"); err != nil {
		t.Fatal(err)
	}
}
