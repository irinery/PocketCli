package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
)

func TestDescribeKeyShowsTypeAndLengthNeverTheSecret(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 36)
	got := describeKey(secret)
	if got != "ghp_…, 40 caracteres" || strings.Contains(got, strings.Repeat("a", 8)) {
		t.Fatalf("descrição vaza ou erra: %q", got)
	}
	if got := describeKey("lin_api_" + strings.Repeat("b", 40)); got != "lin_api_…, 48 caracteres" {
		t.Fatalf("Linear: %q", got)
	}
	if got := describeKey(strings.Repeat("c", 40)); got != "formato não reconhecido, 40 caracteres" {
		t.Fatalf("legado: %q", got)
	}
}

func TestCheckKeyShapeCatchesBadPastesAndWrongSource(t *testing.T) {
	good := "github_pat_" + strings.Repeat("a", 30)
	if err := checkKeyShape("github", good); err != nil {
		t.Fatal(err)
	}
	if err := checkKeyShape("github", strings.Repeat("a", 40)); err != nil {
		t.Fatalf("token legado sem prefixo foi recusado: %v", err)
	}
	for _, test := range []struct{ source, value, want string }{
		{"github", "ghp_abc def", "espaço ou caractere de controle"},
		{"github", "ghp_abc\x1b[200~def", "espaço ou caractere de controle"},
		{"github", "lin_api_" + strings.Repeat("x", 30), "parece ser do linear"},
		{"linear", "ghp_" + strings.Repeat("x", 30), "parece ser do github"},
		{"linear", "github_pat_" + strings.Repeat("x", 30), "parece ser do github"},
	} {
		err := checkKeyShape(test.source, test.value)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s %q: erro %v, esperado %q", test.source, test.value, err, test.want)
		}
		if strings.Contains(err.Error(), strings.Repeat("x", 10)) {
			t.Fatalf("erro vaza o segredo: %v", err)
		}
	}
}

func TestLoadCredentialsTrimsEnvAndRefusesWrongSourceBeforeAnyRequest(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	t.Setenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "  "+token+"\n")
	t.Setenv("POCKETCLI_INVESTIGATE_LINEAR_KEY", "")
	store := secrets.NewSessionStore()
	if err := loadCredentials(store, contract.PlanInput{GitHubEnabled: true}, false); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.WithSecret(context.Background(), "github", func(secret []byte) error { stored = string(secret); return nil }); err != nil || stored != token {
		t.Fatalf("chave não foi aparada: %q %v", stored, err)
	}
	t.Setenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "lin_api_"+strings.Repeat("z", 30))
	err := loadCredentials(secrets.NewSessionStore(), contract.PlanInput{GitHubEnabled: true}, false)
	if err == nil || !strings.Contains(err.Error(), "parece ser do linear") {
		t.Fatalf("aceitou chave do Linear no GitHub: %v", err)
	}
	t.Setenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "ghp_has space")
	if err := loadCredentials(secrets.NewSessionStore(), contract.PlanInput{GitHubEnabled: true}, false); err == nil || !strings.Contains(err.Error(), "espaço") {
		t.Fatalf("aceitou chave com espaço: %v", err)
	}
}

func TestLoadCredentialsUsesGitHubCLISessionWhenEnvIsMissing(t *testing.T) {
	binDir := t.TempDir()
	token := "ghp_" + strings.Repeat("a", 36)
	ghPath := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\nprintf '%s\\n' '"+token+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "")
	t.Setenv("POCKETCLI_INVESTIGATE_LINEAR_KEY", "")
	store := secrets.NewSessionStore()
	if err := loadCredentials(store, contract.PlanInput{GitHubEnabled: true}, false); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.WithSecret(context.Background(), "github", func(secret []byte) error {
		stored = string(secret)
		return nil
	}); err != nil || stored != token {
		t.Fatalf("gh credential not loaded into session: %q %v", stored, err)
	}
	if err := loadCredentials(secrets.NewSessionStore(), contract.PlanInput{LinearEnabled: true}, false); err == nil || !strings.Contains(err.Error(), "POCKETCLI_INVESTIGATE_LINEAR_KEY") {
		t.Fatalf("GitHub CLI token should not satisfy Linear credentials: %v", err)
	}
}
