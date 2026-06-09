package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const vnetDoc = `apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v1 }
spec: { vni: 1 }`

const eniDoc = `apiVersion: dashcenter.v1
kind: Eni
metadata: { name: e1 }
spec:
  vnet_name: v1
  mac_address: "00:11:22:33:44:55"
`

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFilesEmpty(t *testing.T) {
	if _, err := LoadFiles(nil, LoadOpts{}); err == nil {
		t.Fatal()
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "v.yaml", vnetDoc)
	envs, err := LoadFiles([]string{p}, LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Metadata.Name != "v1" {
		t.Fatal()
	}
}

func TestLoadMultiDoc(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "x.yaml", vnetDoc+"\n---\n"+eniDoc)
	envs, err := LoadFiles([]string{p}, LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("got %d", len(envs))
	}
}

func TestLoadDirNonRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", vnetDoc)
	writeFile(t, dir, "b.yml", eniDoc)
	writeFile(t, dir, "ignore.txt", "noise")
	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(sub, 0o700)
	writeFile(t, sub, "c.yaml", vnetDoc) // must NOT be picked

	envs, err := LoadFiles([]string{dir}, LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("got %d", len(envs))
	}
}

func TestLoadDirRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(sub, 0o700)
	writeFile(t, dir, "a.yaml", vnetDoc)
	writeFile(t, sub, "b.json", `{"apiVersion":"dashcenter.v1","kind":"Vnet","metadata":{"name":"v2"},"spec":{"vni":2}}`)

	envs, err := LoadFiles([]string{dir}, LoadOpts{Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("got %d", len(envs))
	}
}

func TestLoadDirEmpty(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadFiles([]string{dir}, LoadOpts{}); err == nil {
		t.Fatal()
	}
}

func TestLoadFileMissing(t *testing.T) {
	if _, err := LoadFiles([]string{filepath.Join(t.TempDir(), "nope")}, LoadOpts{}); err == nil {
		t.Fatal()
	}
}

func TestLoadStdin(t *testing.T) {
	envs, err := LoadFiles([]string{"-"}, LoadOpts{Stdin: bytes.NewBufferString(vnetDoc)})
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 {
		t.Fatal()
	}
}

func TestLoadStdinBadYAML(t *testing.T) {
	if _, err := LoadFiles([]string{"-"}, LoadOpts{Stdin: bytes.NewBufferString("apiVersion: dashcenter.v1\nkind: Foo")}); err == nil {
		t.Fatal()
	}
}

func TestLoadBadFileParses(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "bad.yaml", "kind: : nope")
	if _, err := LoadFiles([]string{p}, LoadOpts{}); err == nil {
		t.Fatal()
	}
}

func TestIsManifestFile(t *testing.T) {
	for _, p := range []string{"a.yaml", "a.YAML", "x.yml", "y.json"} {
		if !isManifestFile(p) {
			t.Errorf("expected yes: %s", p)
		}
	}
	for _, p := range []string{"a.txt", "a", "a.YML.bak"} {
		if isManifestFile(p) {
			t.Errorf("expected no: %s", p)
		}
	}
}

// Deterministic order: two files alphabetically.
func TestLoadDirSortedOrder(t *testing.T) {
	dir := t.TempDir()
	const docA = `apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: alpha }
spec: { vni: 1 }`
	const docB = `apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: bravo }
spec: { vni: 2 }`
	writeFile(t, dir, "b.yaml", docB)
	writeFile(t, dir, "a.yaml", docA)
	envs, err := LoadFiles([]string{dir}, LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if envs[0].Metadata.Name != "alpha" || envs[1].Metadata.Name != "bravo" {
		t.Fatalf("order wrong: %+v", []string{envs[0].Metadata.Name, envs[1].Metadata.Name})
	}
}
