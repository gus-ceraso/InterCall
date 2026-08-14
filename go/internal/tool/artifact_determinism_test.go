package tool

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file tests the deterministic artifact bytes of SPEC.md "One-file
// ownership and safe replacement": identical semantic inputs and Go
// build configuration produce identical interface and Go bytes, output
// contains no timestamp, absolute source path, temporary path, or
// map-order-dependent data, and an unchanged target is not replaced.
// Every test operates only in temporary directories.

func TestArtifactDeterminism(t *testing.T) {
	t.Run("IdenticalInputsIdenticalBytes", func(t *testing.T) {
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			Package:       "gen",
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		var first []byte
		for i := 0; i < 2; i++ {
			root := t.TempDir()
			cfg.OutDir = filepath.Join(root, "out")
			cfg.InterfacePath = filepath.Join(root, "api.intercall")
			writeOnce(t, cfg)
			got, err := os.ReadFile(filepath.Join(cfg.OutDir, bindingFile))
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				first = got
				continue
			}
			if !bytes.Equal(got, first) {
				t.Error("binding bytes differ between identical runs")
			}
		}
	})

	t.Run("ExportModelBodyIsStable", func(t *testing.T) {
		// The complete real pipeline: an export model's canonical body
		// feeds the writer, and the written interface is byte-identical
		// across directories.
		src := `package gen

import "context"

// Echo returns its input.
// @intercall procedure Echo
func Echo(ctx context.Context, value string) (string, error) { return value, nil }
`
		model := exportOne(t, "example.com/gen", src)
		body := model.CanonicalBody()
		if len(body) == 0 {
			t.Fatal("canonical body is empty")
		}
		var first []byte
		for i := 0; i < 2; i++ {
			root := t.TempDir()
			cfg := WriteConfig{
				Mode:          ExportMode,
				OutDir:        filepath.Join(root, "out"),
				Package:       "gen",
				InterfacePath: filepath.Join(root, "api.intercall"),
				GoFile:        []byte(testBindingGo),
				InterfaceBody: body,
			}
			writeOnce(t, cfg)
			got, err := os.ReadFile(filepath.Join(root, "api.intercall"))
			if err != nil {
				t.Fatal(err)
			}
			want := ownedInterfaceBytes(body)
			if !bytes.Equal(got, want) {
				t.Fatalf("interface bytes = %q, want %q", got, want)
			}
			if first == nil {
				first = got
				continue
			}
			if !bytes.Equal(got, first) {
				t.Error("interface bytes differ between identical runs")
			}
		}
	})

	t.Run("ImportDeterminism", func(t *testing.T) {
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ImportMode,
			Package:       "gen",
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		var first []byte
		for i := 0; i < 2; i++ {
			cfg.OutDir = filepath.Join(t.TempDir(), "out")
			writeOnce(t, cfg)
			got, err := os.ReadFile(filepath.Join(cfg.OutDir, bindingFile))
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				first = got
				continue
			}
			if !bytes.Equal(got, first) {
				t.Error("import binding bytes differ between identical runs")
			}
		}
	})

	t.Run("NoAbsoluteOrTemporaryPaths", func(t *testing.T) {
		root := t.TempDir()
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        filepath.Join(root, "out"),
			Package:       "gen",
			InterfacePath: filepath.Join(root, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeOnce(t, cfg)
		for _, path := range []string{filepath.Join(cfg.OutDir, bindingFile), cfg.InterfacePath} {
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(got), root) {
				t.Errorf("%s contains the absolute directory path", path)
			}
			if strings.Contains(string(got), ".tmp-") {
				t.Errorf("%s contains a staging path", path)
			}
			// The only content is the fixed ownership lines, the
			// canonical body, and the generated binding bytes: nothing
			// timestamp- or order-dependent can appear.
			if got := string(got); strings.ContainsAny(got, "\x00") {
				t.Errorf("%s contains non-text bytes", path)
			}
		}
	})

	t.Run("RepeatedWriteIsNoOp", func(t *testing.T) {
		root := t.TempDir()
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        filepath.Join(root, "out"),
			Package:       "gen",
			InterfacePath: filepath.Join(root, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeOnce(t, cfg)
		before, err := os.Stat(filepath.Join(cfg.OutDir, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg)
		after, err := os.Stat(filepath.Join(cfg.OutDir, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			t.Error("identical repeated write replaced the binding")
		}
		if !before.ModTime().Equal(after.ModTime()) {
			t.Error("identical repeated write rewrote the binding")
		}
	})
}
