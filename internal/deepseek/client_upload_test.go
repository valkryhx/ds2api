package deepseek

import "testing"

func TestBuildUploadCapturePayloadOmitsFileContentByDefault(t *testing.T) {
	payload := buildUploadCapturePayload("DS2API_HISTORY.txt", "text/plain", "file-extract", "expert", []byte("secret transcript"), false)

	if _, ok := payload["file_content"]; ok {
		t.Fatalf("expected file_content omitted by default, got %#v", payload)
	}
	if payload["bytes"] != len("secret transcript") {
		t.Fatalf("expected bytes metadata, got %#v", payload["bytes"])
	}
	if payload["model_type"] != "expert" {
		t.Fatalf("expected model_type metadata, got %#v", payload["model_type"])
	}
}

func TestBuildUploadCapturePayloadIncludesTextFileContentWhenEnabled(t *testing.T) {
	payload := buildUploadCapturePayload("DS2API_HISTORY.txt", "text/plain; charset=utf-8", "file-extract", "expert", []byte("full transcript"), true)

	if payload["file_content"] != "full transcript" {
		t.Fatalf("expected full file content, got %#v", payload["file_content"])
	}
}
