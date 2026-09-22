package daemon

import (
	"encoding/base64"
	"testing"
)

func TestDecodeSkillBundleContent(t *testing.T) {
	plain := SkillData{
		ID:      "builtin:goosar-mentioning",
		Name:    "goosar-mentioning",
		Content: "#!/bin/sh\necho dropper",
		Files:   []SkillFileData{{Path: "run.sh", Content: "echo nested"}},
	}
	encoded := SkillData{
		ID:      plain.ID,
		Name:    plain.Name,
		Content: base64.StdEncoding.EncodeToString([]byte(plain.Content)),
		Files: []SkillFileData{{
			Path:    "run.sh",
			Content: base64.StdEncoding.EncodeToString([]byte("echo nested")),
		}},
	}

	got, err := decodeSkillBundle(encoded, skillBundleEncodingBase64)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got.Content != plain.Content {
		t.Fatalf("content did not round-trip: %q", got.Content)
	}
	if got.Files[0].Content != "echo nested" {
		t.Fatalf("file content did not round-trip: %q", got.Files[0].Content)
	}
	if got.Files[0].Path != "run.sh" {
		t.Fatalf("metadata must survive untouched: %q", got.Files[0].Path)
	}
}

func TestDecodeSkillBundleLeavesPlainContentAlone(t *testing.T) {
	plain := SkillData{
		Content: "#!/bin/sh\necho dropper",
		Files:   []SkillFileData{{Path: "run.sh", Content: "echo nested"}},
	}

	got, err := decodeSkillBundle(plain, "")
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got.Content != plain.Content || got.Files[0].Content != "echo nested" {
		t.Fatal("plain content must be returned verbatim when no encoding is declared")
	}
}

func TestDecodeSkillBundleRefusesBrokenBase64(t *testing.T) {
	if _, err := decodeSkillBundle(SkillData{Content: "not base64 !!!"}, skillBundleEncodingBase64); err == nil {
		t.Fatal("expected an error for undecodable content")
	}
}

func TestDecodeSkillBundleRefusesUnknownEncoding(t *testing.T) {
	if _, err := decodeSkillBundle(SkillData{Content: "x"}, "rot13"); err == nil {
		t.Fatal("expected an error for an unknown encoding")
	}
}
