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

func TestComposeDependenciesRequireExplicitLocalService(t *testing.T) {
	content := []byte("services:\n  api:\n    depends_on:\n      - db\n  worker:\n    depends_on:\n      db:\n        condition: service_healthy\n      external_service:\n        condition: service_started\n  db:\n    image: postgres:18\n")
	facts, err := parseManifest("compose", content)
	if err != nil || len(facts.Dependencies) != 2 || facts.Dependencies[0] != (manifestDependency{Service: "api", DependsOn: "db"}) || facts.Dependencies[1] != (manifestDependency{Service: "worker", DependsOn: "db"}) || len(facts.UnresolvedDependencies) != 1 || facts.UnresolvedDependencies[0] != "worker->external_service" {
		t.Fatalf("dependências Compose: %+v err=%v", facts, err)
	}
	if _, err := parseManifest("compose", []byte("services:\n  api:\n    image: one\n    image: two\n")); err == nil {
		t.Fatal("chave Compose duplicada aceita")
	}
	if _, err := parseManifest("compose", []byte("services:\n  api:\n    depends_on: [api]\n")); err == nil {
		t.Fatal("autodependência Compose aceita")
	}
}
