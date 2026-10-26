package core

import "testing"

func TestParseManifestWithoutExecutingContent(t *testing.T) {
	module, err := parseManifest("go_module", []byte("module example.com/acme/api\n\ngo 1.22\n"))
	if err != nil || module.Name != "example.com/acme/api" {
		t.Fatalf("go.mod=%+v err=%v", module, err)
	}
	compose, err := parseManifest("compose", []byte("services:\n  api:\n    image: example/api:1\n  worker:\n    build: .\n"))
	if err != nil || len(compose.Services) != 2 || compose.Services[0] != "api" || compose.Services[1] != "worker" {
		t.Fatalf("compose=%+v err=%v", compose, err)
	}
	if _, err := parseManifest("compose", []byte("services:\n  api: &x {image: a}\n  worker: *x\n")); err == nil {
		t.Fatal("alias YAML aceito")
	}
	openAPI, err := parseManifest("api_contract", []byte(`{"openapi":"3.1.0","info":{"title":"API de pedidos"}}`))
	if err != nil || openAPI.Name != "API de pedidos" {
		t.Fatalf("openapi=%+v err=%v", openAPI, err)
	}
}
