// Package cli holds reusable command helpers — manifest discovery,
// stdin handling, label selector parsing, $EDITOR invocation, etc.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

// LoadOpts controls LoadFiles.
type LoadOpts struct {
	Recursive bool
	Stdin     io.Reader // for "-" argument; nil → os.Stdin
}

// LoadFiles walks every -f argument and returns parsed envelopes in
// deterministic (lexicographic) order. Args may be files, directories,
// or "-" (stdin). Recursive walks all *.yaml / *.yml / *.json under a
// directory; non-recursive walks only top-level matches.
func LoadFiles(args []string, opts LoadOpts) ([]*manifest.Envelope, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("manifest: no -f arguments")
	}
	var out []*manifest.Envelope
	for _, a := range args {
		envs, err := loadOne(a, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, envs...)
	}
	return out, nil
}

func loadOne(arg string, opts LoadOpts) ([]*manifest.Envelope, error) {
	if arg == "-" {
		r := opts.Stdin
		if r == nil {
			r = os.Stdin
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("manifest: stdin: %w", err)
		}
		envs, err := manifest.ParseMulti(data)
		if err != nil {
			return nil, fmt.Errorf("manifest: stdin: %w", err)
		}
		return envs, nil
	}
	info, err := os.Stat(arg)
	if err != nil {
		return nil, fmt.Errorf("manifest: stat %s: %w", arg, err)
	}
	if info.IsDir() {
		return loadDir(arg, opts.Recursive)
	}
	data, err := os.ReadFile(arg)
	if err != nil {
		return nil, fmt.Errorf("manifest: read %s: %w", arg, err)
	}
	envs, err := manifest.ParseMulti(data)
	if err != nil {
		return nil, fmt.Errorf("manifest: %s: %w", arg, err)
	}
	return envs, nil
}

func loadDir(dir string, recursive bool) ([]*manifest.Envelope, error) {
	files, err := discoverManifests(dir, recursive)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("manifest: no .yaml/.yml/.json files under %s", dir)
	}
	var out []*manifest.Envelope
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("manifest: read %s: %w", f, err)
		}
		envs, err := manifest.ParseMulti(data)
		if err != nil {
			return nil, fmt.Errorf("manifest: %s: %w", f, err)
		}
		out = append(out, envs...)
	}
	return out, nil
}

func discoverManifests(dir string, recursive bool) ([]string, error) {
	var files []string
	if recursive {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if isManifestFile(path) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if isManifestFile(p) {
				files = append(files, p)
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

func isManifestFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
		return true
	}
	return false
}
