package tool

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// generatedInterfaceID extracts and checks the one emitted InterfaceID
// literal, proving that generated metadata is the digest of the canonical
// body rather than the input spelling or generated Go source.
func generatedInterfaceID(t *testing.T, goFile, body []byte) []byte {
	t.Helper()
	src := string(goFile)
	const prefix = "intercall.InterfaceID{\n"
	start := strings.Index(src, prefix)
	if start < 0 {
		t.Fatal("generated source has no InterfaceID literal")
	}
	endRel := strings.Index(src[start+len(prefix):], "},")
	if endRel < 0 {
		t.Fatal("generated InterfaceID literal is not deterministically wrapped")
	}
	literal := src[start+len(prefix) : start+len(prefix)+endRel]
	var got []byte
	for _, line := range strings.Split(literal, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "0x") || !strings.HasSuffix(line, ",") {
			t.Fatalf("unexpected InterfaceID literal line %q", line)
		}
		value, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(line, "0x"), ","), 16, 8)
		if err != nil {
			t.Fatalf("parsing InterfaceID literal line %q: %v", line, err)
		}
		got = append(got, byte(value))
	}
	if len(got) != sha256.Size {
		t.Fatalf("emitted InterfaceID has %d bytes, want %d", len(got), sha256.Size)
	}
	want := sha256.Sum256(body)
	if !bytes.Equal(got, want[:]) {
		t.Fatalf("emitted InterfaceID = %x, want sha256(body) %x", got, want)
	}
	return got
}

func TestGeneratedInterfaceIDs(t *testing.T) {
	t.Run("import and export share the canonical digest", func(t *testing.T) {
		src := []byte("exception internal_exception;\nexception invalid_arguments;\nexception procedure_not_found;\n")
		importFile, importBody, err := GenerateImport("id.intercall", src, nil, "imp")
		if err != nil {
			t.Fatalf("GenerateImport: %v", err)
		}
		model, err := MapExport(&DiscoverResult{}, "")
		if err != nil {
			t.Fatalf("MapExport: %v", err)
		}
		exportFile, exportBody, err := GenerateExport(model, "exp")
		if err != nil {
			t.Fatalf("GenerateExport: %v", err)
		}
		if !bytes.Equal(importBody, exportBody) {
			t.Fatalf("canonical bodies differ: import %q, export %q", importBody, exportBody)
		}
		importID := generatedInterfaceID(t, importFile, importBody)
		exportID := generatedInterfaceID(t, exportFile, exportBody)
		if !bytes.Equal(importID, exportID) {
			t.Fatalf("import ID %x differs from export ID %x", importID, exportID)
		}
	})

	t.Run("formatting preserves ID and semantics change it", func(t *testing.T) {
		first, firstBody, err := GenerateImport("id.intercall", []byte("procedure ping {};"), nil, "imp")
		if err != nil {
			t.Fatalf("first GenerateImport: %v", err)
		}
		second, secondBody, err := GenerateImport("id.intercall", []byte("procedure  ping{};"), nil, "imp")
		if err != nil {
			t.Fatalf("second GenerateImport: %v", err)
		}
		if !bytes.Equal(firstBody, secondBody) {
			t.Fatalf("format-equivalent bodies differ: %q vs %q", firstBody, secondBody)
		}
		firstID := generatedInterfaceID(t, first, firstBody)
		secondID := generatedInterfaceID(t, second, secondBody)
		if !bytes.Equal(firstID, secondID) {
			t.Fatalf("format-equivalent IDs differ: %x vs %x", firstID, secondID)
		}

		third, thirdBody, err := GenerateImport("id.intercall", []byte("procedure pong {};"), nil, "imp")
		if err != nil {
			t.Fatalf("third GenerateImport: %v", err)
		}
		thirdID := generatedInterfaceID(t, third, thirdBody)
		if bytes.Equal(firstBody, thirdBody) || bytes.Equal(firstID, thirdID) {
			t.Fatalf("semantic change retained body/ID: body %q, ID %s", thirdBody, fmt.Sprintf("%x", thirdID))
		}
	})
}
