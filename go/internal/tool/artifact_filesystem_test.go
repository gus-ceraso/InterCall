package tool

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file tests the host-filesystem safety of SPEC.md "One-file
// ownership and safe replacement": exact written bytes, output creation
// ordering, output package rules, Go-named collisions, symlink /
// directory / device / FIFO leaf rejection, host filename equivalence,
// mode/package/stamp ownership, preservation of unrelated non-Go files
// and hard links, unchanged inode on no-op, no deletion or truncation,
// and replacement failure safety. Every test operates only in
// temporary directories.

func TestArtifactFilesystemSafety(t *testing.T) {
	t.Run("FreshExportExactBytes", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeOnce(t, cfg)

		// The binding is exactly the two ownership lines, one blank
		// line, and the generated body; the interface is exactly the
		// marker, one blank line, and the canonical body.
		stamp := ArtifactStamp(body)
		gotBinding, err := os.ReadFile(filepath.Join(out, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		if want := composedBinding(ExportMode, stamp, []byte(testBindingGo)); !bytes.Equal(gotBinding, want) {
			t.Errorf("binding bytes = %q, want %q", gotBinding, want)
		}
		gotIntf, err := os.ReadFile(filepath.Join(intfDir, "api.intercall"))
		if err != nil {
			t.Fatal(err)
		}
		if want := ownedInterfaceBytes(body); !bytes.Equal(gotIntf, want) {
			t.Errorf("interface bytes = %q, want %q", gotIntf, want)
		}
		// The stamp records the body hash in both targets.
		if !bytes.Contains(gotBinding, []byte("sha256:"+stamp)) || !bytes.Contains(gotIntf, []byte("sha256:"+stamp)) {
			t.Error("artifact stamp missing from a target")
		}
		// The output directory holds exactly the binding.
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != bindingFile {
			t.Errorf("output directory entries = %v, want only %s", entries, bindingFile)
		}
		// The artifact files are world-readable.
		info, err := os.Stat(filepath.Join(out, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Errorf("binding mode = %o, want 0644", perm)
		}
		noTemps(t, out)
		noTemps(t, intfDir)
	})

	t.Run("EmptyInterfaceBody", func(t *testing.T) {
		// An empty canonical interface is a valid interface; the owned
		// file is the marker and the blank line, and the stamp hashes
		// the zero-byte body.
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intf := filepath.Join(root, "api.intercall")
		writeOnce(t, WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: intf,
			GoFile:        []byte(testBindingGo),
		})
		got, err := os.ReadFile(intf)
		if err != nil {
			t.Fatal(err)
		}
		if want := ownedInterfaceBytes(nil); !bytes.Equal(got, want) {
			t.Errorf("interface bytes = %q, want %q", got, want)
		}
		// The zero-byte body round-trips through the ownership check.
		if io, ok := parseInterfaceOwnership(got); !ok || io.stamp != ArtifactStamp(nil) {
			t.Errorf("empty owned interface not recognized: %+v, %v", io, ok)
		}
	})

	t.Run("FreshImportExactBytes", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ImportMode,
			OutDir:        out,
			Package:       "gen",
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeOnce(t, cfg)
		stamp := ArtifactStamp(body)
		got, err := os.ReadFile(filepath.Join(out, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		if want := composedBinding(ImportMode, stamp, []byte(testBindingGo)); !bytes.Equal(got, want) {
			t.Errorf("binding bytes = %q, want %q", got, want)
		}
		// The import binding stamp also hashes the canonical body.
		if !bytes.Contains(got, []byte("// intercall-go binding: import sha256:"+stamp)) {
			t.Error("import binding line missing or wrong")
		}
	})

	t.Run("InterfaceInsideOutputDirectory", func(t *testing.T) {
		// The interface target may live inside --out; it is a non-Go
		// entry and is preserved by the directory scan.
		root := t.TempDir()
		out := filepath.Join(root, "out")
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(out, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeOnce(t, cfg)
		if _, err := os.Stat(filepath.Join(out, bindingFile)); err != nil {
			t.Errorf("binding missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(out, "api.intercall")); err != nil {
			t.Errorf("interface missing: %v", err)
		}
	})

	t.Run("SymlinkedParentsResolve", func(t *testing.T) {
		// Both target parents are resolved through the host filesystem
		// and the write operates on the resolved directories.
		root := t.TempDir()
		real := filepath.Join(root, "real")
		intfReal := filepath.Join(root, "intf-real")
		for _, d := range []string{real, intfReal} {
			if err := os.MkdirAll(d, 0o777); err != nil {
				t.Fatal(err)
			}
		}
		outLink := filepath.Join(root, "out-link")
		intfLink := filepath.Join(root, "intf-link")
		if err := os.Symlink(real, outLink); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(intfReal, intfLink); err != nil {
			t.Fatal(err)
		}
		body := canonicalBody(t, testInterfaceSrc1)
		writeOnce(t, WriteConfig{
			Mode:          ExportMode,
			OutDir:        outLink,
			Package:       "gen",
			InterfacePath: filepath.Join(intfLink, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		})
		if _, err := os.Stat(filepath.Join(real, bindingFile)); err != nil {
			t.Errorf("binding missing in resolved output directory: %v", err)
		}
		if _, err := os.Stat(filepath.Join(intfReal, "api.intercall")); err != nil {
			t.Errorf("interface missing in resolved parent: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(outLink, bindingFile)); err != nil {
			t.Errorf("symlink leaf created at the link itself: %v", err)
		}
	})

	t.Run("ValidationFailureCreatesNothing", func(t *testing.T) {
		root := t.TempDir()
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		base := WriteConfig{
			Mode:          ExportMode,
			OutDir:        filepath.Join(root, "out"),
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: canonicalBody(t, testInterfaceSrc1),
		}

		// Generated-content validation finishes before output-directory
		// creation: a failure creates no directory and no file.
		cases := []struct {
			name string
			cfg  WriteConfig
		}{
			{"InvalidGo", WriteConfig{Mode: base.Mode, OutDir: base.OutDir, Package: base.Package, InterfacePath: base.InterfacePath, GoFile: []byte("package gen\nfunc {\n"), InterfaceBody: base.InterfaceBody}},
			{"InvalidInterfaceBody", WriteConfig{Mode: base.Mode, OutDir: base.OutDir, Package: base.Package, InterfacePath: base.InterfacePath, GoFile: base.GoFile, InterfaceBody: []byte("type broken {\n")}},
			{"PackageMismatch", WriteConfig{Mode: base.Mode, OutDir: base.OutDir, Package: "other", InterfacePath: base.InterfacePath, GoFile: base.GoFile, InterfaceBody: base.InterfaceBody}},
			{"InvalidPackage", WriteConfig{Mode: base.Mode, OutDir: base.OutDir, Package: "123bad", InterfacePath: base.InterfacePath, GoFile: base.GoFile, InterfaceBody: base.InterfaceBody}},
			{"NoGoFile", WriteConfig{Mode: base.Mode, OutDir: base.OutDir, Package: base.Package, InterfacePath: base.InterfacePath, InterfaceBody: base.InterfaceBody}},
		}
		for _, tc := range cases {
			dir := filepath.Join(root, "case-"+tc.name)
			tc.cfg.OutDir = dir
			writeFails(t, tc.cfg)
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("%s: output directory was created despite validation failure", tc.name)
			}
		}
	})

	t.Run("MissingInterfaceParent", func(t *testing.T) {
		// The output directory is created before the interface target's
		// parent is required to exist.
		root := t.TempDir()
		out := filepath.Join(root, "out")
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(root, "missing-dir", "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeFails(t, cfg, "parent directory")
		if info, err := os.Stat(out); err != nil || !info.IsDir() {
			t.Errorf("output directory not created: %v", err)
		}
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("output directory contains %v after failure", entries)
		}
		if _, err := os.Stat(filepath.Join(root, "missing-dir", "api.intercall")); !os.IsNotExist(err) {
			t.Error("interface file created despite missing parent")
		}

		// A parent that is a regular file is an error too.
		parentFile := filepath.Join(root, "parent-file")
		if err := os.WriteFile(parentFile, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		cfg.InterfacePath = filepath.Join(parentFile, "api.intercall")
		writeFails(t, cfg, "not a directory")
	})

	t.Run("GoFileCollisions", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		body := canonicalBody(t, testInterfaceSrc1)

		// A regular Go file is a collision.
		other := filepath.Join(out, "other.go")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(other, []byte("package other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(other)
		if err != nil {
			t.Fatal(err)
		}
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeFails(t, cfg, "other.go", "Go file")
		if _, err := os.Stat(filepath.Join(out, bindingFile)); !os.IsNotExist(err) {
			t.Error("binding created despite the Go collision")
		}
		after, err := os.Stat(other)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			t.Error("colliding Go file was touched")
		}

		// A Go-named directory is a collision.
		if err := os.Remove(other); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(out, "sub.go"), 0o777); err != nil {
			t.Fatal(err)
		}
		writeFails(t, cfg, "sub.go", "Go-named nonregular entry")

		// A case-variant ".GO" name is a Go file only under host
		// filename equivalence.
		if err := os.Remove(filepath.Join(out, "sub.go")); err != nil {
			t.Fatal(err)
		}
		variant := filepath.Join(out, "OTHER.GO")
		if err := os.WriteFile(variant, []byte("package other\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if hostFold(out) != nil {
			writeFails(t, cfg, "OTHER.GO")
		} else {
			writeOnce(t, cfg)
			// The case-variant name is a non-Go entry on this host and
			// is preserved.
			if got, err := os.ReadFile(variant); err != nil || string(got) != "package other\n" {
				t.Errorf("OTHER.GO not preserved: %v", err)
			}
		}
	})

	t.Run("LeafRejections", func(t *testing.T) {
		body := canonicalBody(t, testInterfaceSrc1)
		newCfg := func(out, intf string) WriteConfig {
			return WriteConfig{
				Mode:          ExportMode,
				OutDir:        out,
				Package:       "gen",
				InterfacePath: intf,
				GoFile:        []byte(testBindingGo),
				InterfaceBody: body,
			}
		}

		// A symlink at the binding leaf is an error.
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(out, bindingFile)); err != nil {
			t.Fatal(err)
		}
		writeFails(t, newCfg(out, filepath.Join(intfDir, "api.intercall")), "symbolic link")

		// A symlink at the interface leaf is an error.
		root2 := t.TempDir()
		out2 := filepath.Join(root2, "out")
		intfDir2 := filepath.Join(root2, "interfaces")
		if err := os.MkdirAll(intfDir2, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root2, "nowhere"), filepath.Join(intfDir2, "api.intercall")); err != nil {
			t.Fatal(err)
		}
		writeFails(t, newCfg(out2, filepath.Join(intfDir2, "api.intercall")), "symbolic link")

		// A directory at either leaf is an error.
		root3 := t.TempDir()
		out3 := filepath.Join(root3, "out")
		intfDir3 := filepath.Join(root3, "interfaces")
		if err := os.MkdirAll(filepath.Join(out3, bindingFile), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(intfDir3, 0o777); err != nil {
			t.Fatal(err)
		}
		writeFails(t, newCfg(out3, filepath.Join(intfDir3, "api.intercall")), "directory")
		root4 := t.TempDir()
		out4 := filepath.Join(root4, "out")
		if err := os.MkdirAll(filepath.Join(out4), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(out4, "api.intercall"), 0o777); err != nil {
			t.Fatal(err)
		}
		writeFails(t, newCfg(out4, filepath.Join(out4, "api.intercall")), "directory")

		// A symlink at the binding leaf is also rejected for import.
		root5 := t.TempDir()
		out5 := filepath.Join(root5, "out")
		if err := os.MkdirAll(out5, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root5, "nowhere"), filepath.Join(out5, bindingFile)); err != nil {
			t.Fatal(err)
		}
		writeFails(t, WriteConfig{
			Mode:          ImportMode,
			OutDir:        out5,
			Package:       "gen",
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}, "symbolic link")
	})

	t.Run("InterfaceTargetNameRules", func(t *testing.T) {
		body := canonicalBody(t, testInterfaceSrc1)
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		newCfg := func(intf string) WriteConfig {
			return WriteConfig{
				Mode:          ExportMode,
				OutDir:        out,
				Package:       "gen",
				InterfacePath: intf,
				GoFile:        []byte(testBindingGo),
				InterfaceBody: body,
			}
		}

		// A ".go" filename is rejected, including the exact generated
		// target name.
		writeFails(t, newCfg(filepath.Join(intfDir, "api.go")), "must not have a .go filename")
		writeFails(t, newCfg(filepath.Join(intfDir, "binding_gen.go")), "must not have a .go filename")
		writeFails(t, newCfg(filepath.Join(out, bindingFile)), "must not have a .go filename")

		// A case variant of the generated target name is the generated
		// target under host filename equivalence.
		variant := filepath.Join(intfDir, "BINDING_GEN.GO")
		if hostFold(intfDir) != nil {
			writeFails(t, newCfg(variant), "filename equivalence")
		} else {
			writeOnce(t, newCfg(variant))
			if _, err := os.Stat(variant); err != nil {
				t.Errorf("interface target not written: %v", err)
			}
		}

		// A case-variant ".GO" interface name follows the host's
		// equivalence too.
		upper := filepath.Join(intfDir, "API.GO")
		if hostFold(intfDir) != nil {
			writeFails(t, newCfg(upper), "must not have a .go filename")
		} else {
			writeOnce(t, newCfg(upper))
			if _, err := os.Stat(upper); err != nil {
				t.Errorf("interface target not written: %v", err)
			}
		}
	})

	t.Run("NonGoEntriesPreserved", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		notes := filepath.Join(out, "notes.txt")
		link := filepath.Join(out, "notes2.txt")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(notes, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(notes, link); err != nil {
			t.Fatal(err)
		}
		notesBefore, err := os.Stat(notes)
		if err != nil {
			t.Fatal(err)
		}
		linkBefore, err := os.Stat(link)
		if err != nil {
			t.Fatal(err)
		}
		body := canonicalBody(t, testInterfaceSrc1)
		writeOnce(t, WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		})
		if got, err := os.ReadFile(notes); err != nil || string(got) != "keep me" {
			t.Errorf("notes.txt not preserved: %q, %v", got, err)
		}
		notesAfter, err := os.Stat(notes)
		if err != nil {
			t.Fatal(err)
		}
		linkAfter, err := os.Stat(link)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(notesBefore, notesAfter) || !os.SameFile(linkBefore, linkAfter) {
			t.Error("non-Go entry or its hard link changed inode")
		}
		// The unrelated hard-link pair is preserved as a pair.
		if !os.SameFile(notesAfter, linkAfter) {
			t.Error("hard-link pair broken")
		}
	})

	t.Run("HardLinkToOwnedTargetKeepsOldBytes", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		body1 := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body1,
		}
		writeOnce(t, cfg)
		link := filepath.Join(out, "link.txt")
		if err := os.Link(filepath.Join(out, bindingFile), link); err != nil {
			t.Fatal(err)
		}
		linkBefore, err := os.Stat(link)
		if err != nil {
			t.Fatal(err)
		}
		// A second write replaces the owned target by rename; the hard
		// link retains the old inode and bytes.
		body2 := canonicalBody(t, testInterfaceSrc2)
		cfg.InterfaceBody = body2
		writeOnce(t, cfg)
		got, err := os.ReadFile(link)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, composedBinding(ExportMode, ArtifactStamp(body1), []byte(testBindingGo))) {
			t.Errorf("hard link lost the old inode bytes: %q", got)
		}
		linkAfter, err := os.Stat(link)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(linkBefore, linkAfter) {
			t.Error("hard link inode changed")
		}
		newBinding, err := os.ReadFile(filepath.Join(out, bindingFile))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(newBinding, []byte(ArtifactStamp(body2))) {
			t.Error("binding not replaced with the new stamp")
		}
	})

	t.Run("UnchangedTargetNotReplaced", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		body := canonicalBody(t, testInterfaceSrc1)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body,
		}
		writeOnce(t, cfg)
		bindingPath := filepath.Join(out, bindingFile)
		intfPath := filepath.Join(intfDir, "api.intercall")
		before, err := os.Stat(bindingPath)
		if err != nil {
			t.Fatal(err)
		}
		intfBefore, err := os.Stat(intfPath)
		if err != nil {
			t.Fatal(err)
		}
		writeOnce(t, cfg)
		after, err := os.Stat(bindingPath)
		if err != nil {
			t.Fatal(err)
		}
		intfAfter, err := os.Stat(intfPath)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) || !os.SameFile(intfBefore, intfAfter) {
			t.Error("unchanged target was replaced")
		}
		if !before.ModTime().Equal(after.ModTime()) || !intfBefore.ModTime().Equal(intfAfter.ModTime()) {
			t.Error("unchanged target was rewritten")
		}
	})

	t.Run("OwnershipRejections", func(t *testing.T) {
		root := t.TempDir()
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		body1 := canonicalBody(t, testInterfaceSrc1)
		body2 := canonicalBody(t, testInterfaceSrc2)
		newCfg := func(out string, body []byte) WriteConfig {
			return WriteConfig{
				Mode:          ExportMode,
				OutDir:        out,
				Package:       "gen",
				InterfacePath: filepath.Join(intfDir, "api.intercall"),
				GoFile:        []byte(testBindingGo),
				InterfaceBody: body,
			}
		}

		// A handwritten binding is not replaceable.
		out := filepath.Join(root, "handwritten")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		hand := filepath.Join(out, bindingFile)
		if err := os.WriteFile(hand, []byte("package gen\n\nvar x = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFails(t, newCfg(out, body1), "not replaceable")
		if got, _ := os.ReadFile(hand); string(got) != "package gen\n\nvar x = 1\n" {
			t.Error("handwritten binding changed")
		}

		// A binding of the other mode is not replaceable.
		out = filepath.Join(root, "wrongmode")
		writeOnce(t, WriteConfig{
			Mode:    ImportMode,
			OutDir:  out,
			Package: "gen",
			GoFile:  []byte(testBindingGo),
		})
		writeFails(t, newCfg(out, body1), "import binding")

		// A binding whose package does not match the invocation is not
		// replaceable.
		out = filepath.Join(root, "wrongpkg")
		writeOnce(t, WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "other.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body1,
		})
		cfg := newCfg(out, body2)
		cfg.Package = "other"
		writeFails(t, cfg, "package")

		// A binding with an invalid artifact stamp (uppercase hex) is
		// not replaceable.
		out = filepath.Join(root, "badstamp")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		bad := filepath.Join(out, bindingFile)
		if err := os.WriteFile(bad, composedBinding(ExportMode, strings.ToUpper(strings.Repeat("a", artifactStampLen)), []byte(testBindingGo)), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFails(t, newCfg(out, body1), "not replaceable")

		// A binding whose stamp has valid syntax is replaceable even
		// when the stamp is not the body's digest: the binding check
		// requires valid syntax only, unlike the interface target whose
		// stamp must match its canonical body.
		out = filepath.Join(root, "wrongstamp")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		wrong := filepath.Join(out, bindingFile)
		if err := os.WriteFile(wrong, composedBinding(ExportMode, ArtifactStamp(canonicalBody(t, testInterfaceSrc2)), []byte(testBindingGo)), 0o644); err != nil {
			t.Fatal(err)
		}
		wsCfg := newCfg(out, body1)
		wsCfg.InterfacePath = filepath.Join(intfDir, "wrongstamp.intercall")
		writeOnce(t, wsCfg)
		if got, _ := os.ReadFile(wrong); !bytes.Contains(got, []byte("sha256:"+ArtifactStamp(body1))) {
			t.Error("valid-syntax binding was not replaced")
		}

		// An unmarked interface file is not replaceable.
		out = filepath.Join(root, "unmarked-intf")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		umIntf := filepath.Join(intfDir, "unmarked.intercall")
		umCfg := newCfg(out, body1)
		umCfg.InterfacePath = umIntf
		writeOnce(t, umCfg)
		if err := os.WriteFile(umIntf, []byte("exception done;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		umCfg2 := newCfg(out, body2)
		umCfg2.InterfacePath = umIntf
		writeFails(t, umCfg2, "not replaceable", "ownership marker")
		if got, _ := os.ReadFile(umIntf); string(got) != "exception done;\n" {
			t.Error("unmarked interface changed")
		}

		// A marked interface whose stamp does not match its body is not
		// replaceable: the tool never overwrites a tampered file.
		out = filepath.Join(root, "tampered-intf")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		tmIntf := filepath.Join(intfDir, "tampered.intercall")
		tmCfg := newCfg(out, body1)
		tmCfg.InterfacePath = tmIntf
		writeOnce(t, tmCfg)
		tampered := append(ownedInterfaceBytes(body1), ' ')
		if err := os.WriteFile(tmIntf, tampered, 0o644); err != nil {
			t.Fatal(err)
		}
		tmCfg2 := newCfg(out, body2)
		tmCfg2.InterfacePath = tmIntf
		writeFails(t, tmCfg2, "not replaceable", "stamp")
		if got, _ := os.ReadFile(tmIntf); !bytes.Equal(got, tampered) {
			t.Error("tampered interface changed")
		}

		// A marked interface without the blank line after the marker is
		// not replaceable.
		out = filepath.Join(root, "noblank")
		if err := os.MkdirAll(out, 0o777); err != nil {
			t.Fatal(err)
		}
		nbIntf := filepath.Join(intfDir, "noblank.intercall")
		nbCfg := newCfg(out, body1)
		nbCfg.InterfacePath = nbIntf
		writeOnce(t, nbCfg)
		noblank := []byte(interfaceMarkerPrefix + ArtifactStamp(body1) + interfaceMarkerSuffix + "\n" + string(body1))
		if err := os.WriteFile(nbIntf, noblank, 0o644); err != nil {
			t.Fatal(err)
		}
		nbCfg2 := newCfg(out, body2)
		nbCfg2.InterfacePath = nbIntf
		writeFails(t, nbCfg2, "not replaceable")
	})

	t.Run("ReplacementFailureSafety", func(t *testing.T) {
		root := t.TempDir()
		out := filepath.Join(root, "out")
		intfDir := filepath.Join(root, "interfaces")
		if err := os.MkdirAll(intfDir, 0o777); err != nil {
			t.Fatal(err)
		}
		body1 := canonicalBody(t, testInterfaceSrc1)
		body2 := canonicalBody(t, testInterfaceSrc2)
		cfg := WriteConfig{
			Mode:          ExportMode,
			OutDir:        out,
			Package:       "gen",
			InterfacePath: filepath.Join(intfDir, "api.intercall"),
			GoFile:        []byte(testBindingGo),
			InterfaceBody: body1,
		}
		writeOnce(t, cfg)
		bindingPath := filepath.Join(out, bindingFile)
		intfPath := filepath.Join(intfDir, "api.intercall")
		bindingBefore, err := os.Stat(bindingPath)
		if err != nil {
			t.Fatal(err)
		}
		intfBefore, err := os.Stat(intfPath)
		if err != nil {
			t.Fatal(err)
		}

		// When the host cannot replace an existing file by rename, the
		// command fails without first deleting it, and the staged files
		// are cleaned up. The rename seam simulates the host failure.
		cfg.InterfaceBody = body2
		cfg.CheckGo = NewImportGoChecker()
		w := &artifactWriter{cfg: cfg, rename: func(old, new string) error {
			return &os.LinkError{Op: "rename", Old: old, New: new, Err: fs.ErrPermission}
		}}
		err = w.run()
		if err == nil || !strings.Contains(err.Error(), "not deleted") {
			t.Fatalf("replacement error = %v, want a not-deleted failure", err)
		}
		// The diagnostic names the logical target at line 1, column 1
		// and carries only the underlying cause: the staging path and
		// the seam's physical paths never appear.
		var de *Error
		if !errors.As(err, &de) || de.Filename != filepath.Join(out, bindingFile) || de.Pos != (Position{Line: 1, Column: 1}) {
			t.Errorf("replacement error = %v, want a 1:1 diagnostic on the binding target", err)
		}
		if strings.Contains(err.Error(), ".tmp-") {
			t.Errorf("replacement error reports a staging path: %v", err)
		}
		bindingAfter, err := os.Stat(bindingPath)
		if err != nil {
			t.Fatal(err)
		}
		intfAfter, err := os.Stat(intfPath)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(bindingBefore, bindingAfter) || !os.SameFile(intfBefore, intfAfter) {
			t.Error("a target was replaced or deleted despite the rename failure")
		}
		if got, _ := os.ReadFile(bindingPath); !bytes.Equal(got, composedBinding(ExportMode, ArtifactStamp(body1), []byte(testBindingGo))) {
			t.Error("binding bytes changed despite the rename failure")
		}
		noTemps(t, out)
		noTemps(t, intfDir)

		// A staging failure before any replacement also leaves every
		// target untouched: both targets are staged before the first
		// rename. A read-only interface parent forces the staging
		// failure; root bypasses permission checks and is skipped.
		if os.Geteuid() == 0 {
			t.Skip("running as root: read-only directories do not block staging")
		}
		// The interface parent is a symlink, so the logical operand and
		// the resolved physical directory are distinct names: the
		// staging diagnostic must name the logical target only and must
		// never report the staging path or the resolved directory.
		intfLink := filepath.Join(root, "intf-link")
		if err := os.Symlink(intfDir, intfLink); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(intfDir, 0o555); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(intfDir, 0o755)
		stagingCfg := cfg
		stagingCfg.InterfacePath = filepath.Join(intfLink, "api.intercall")
		stagingCfg.CheckGo = NewImportGoChecker()
		err = WriteArtifacts(stagingCfg)
		if err == nil {
			t.Fatal("write succeeded into a read-only interface directory")
		}
		var se *Error
		if !errors.As(err, &se) || se.Filename != stagingCfg.InterfacePath || se.Pos != (Position{Line: 1, Column: 1}) {
			t.Errorf("staging error = %v, want a 1:1 diagnostic on the logical interface target", err)
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("staging error = %v, want the underlying cause", err)
		}
		if strings.Contains(err.Error(), ".tmp-") || strings.Contains(err.Error(), filepath.Base(intfDir)) {
			t.Errorf("staging error reports a staging path or the resolved physical directory: %v", err)
		}
		bindingAfter2, err := os.Stat(bindingPath)
		if err != nil {
			t.Fatal(err)
		}
		intfAfter2, err := os.Stat(intfPath)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(bindingBefore, bindingAfter2) || !os.SameFile(intfBefore, intfAfter2) {
			t.Error("a target was replaced despite the staging failure")
		}
		noTemps(t, out)
		noTemps(t, intfDir)
	})
}
