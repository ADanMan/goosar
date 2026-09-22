package handler

import (
	"encoding/base64"
	"testing"

	"github.com/adanman/goosar/server/internal/service"
)

func TestEncodeSkillBundlesBase64(t *testing.T) {
	bundles := []service.AgentSkillData{{
		ID:      "builtin:goosar-mentioning",
		Name:    "goosar-mentioning",
		Content: "#!/bin/sh\necho dropper",
		Files: []service.AgentSkillFileData{
			{Path: "run.sh", Content: "echo nested"},
		},
	}}

	encoded := encodeSkillBundleContent(bundles)

	if encoded[0].Content == bundles[0].Content {
		t.Fatal("skill content was not encoded — the scanner still sees a script")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded[0].Content)
	if err != nil {
		t.Fatalf("content is not valid base64: %v", err)
	}
	if string(decoded) != "#!/bin/sh\necho dropper" {
		t.Fatalf("content did not survive the round trip: %q", decoded)
	}

	fileDecoded, err := base64.StdEncoding.DecodeString(encoded[0].Files[0].Content)
	if err != nil {
		t.Fatalf("file content is not valid base64: %v", err)
	}
	if string(fileDecoded) != "echo nested" {
		t.Fatalf("file content did not survive the round trip: %q", fileDecoded)
	}

	if encoded[0].ID != "builtin:goosar-mentioning" || encoded[0].Name != "goosar-mentioning" {
		t.Fatal("metadata must not be encoded")
	}
	if encoded[0].Files[0].Path != "run.sh" {
		t.Fatal("file path must not be encoded")
	}

	if bundles[0].Content != "#!/bin/sh\necho dropper" {
		t.Fatal("encoding mutated the caller's bundles")
	}
}
