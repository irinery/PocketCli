package catalogue

import (
	"os"
	"path/filepath"
)

func ensureSafeDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return newError("ERR_LOCAL_DIR_UNAVAILABLE", "diretorio local indisponivel", err.Error(), "crie o diretorio com permissao 0700")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return newError("ERR_SYMLINK_BLOCKED", "symlink bloqueado em area sensivel", path, "use diretorio real dentro de ~/.pocketcli")
	}
	if !info.IsDir() {
		return newError("ERR_LOCAL_DIR_UNAVAILABLE", "path local nao e diretorio", path, "remova o arquivo e crie diretorio 0700")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return newError("ERR_INSECURE_LOCAL_DIR_PERMISSIONS", "permissao insegura em diretorio local", path, "rode chmod 700 "+path)
	}
	return nil
}

func ensureSafeFile(path string) error {
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err != nil {
		return newError("ERR_CATALOGUE_PARSE", "arquivo local indisponivel", err.Error(), "corrija o arquivo local")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return newError("ERR_SYMLINK_BLOCKED", "symlink bloqueado em arquivo local", clean, "use arquivo real")
	}
	if info.IsDir() {
		return newError("ERR_CATALOGUE_PARSE", "catalogo local aponta para diretorio", clean, "use arquivo JSON")
	}
	if info.Mode().Perm()&0o002 != 0 {
		return newError("ERR_INSECURE_LOCAL_DIR_PERMISSIONS", "arquivo local world-writable", clean, "remova permissao de escrita global")
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}
