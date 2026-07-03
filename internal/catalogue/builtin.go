package catalogue

func BuiltinRecipes() []Recipe {
	recipes := []Recipe{
		argv("ssh.list", "Listar arquivos SSH", "ssh", RiskSensitive, "ls", []string{"-al", "~/.ssh"}, nil, "ssh", "keys"),
		argv("ssh.public-key", "Mostrar chave publica SSH padrao", "ssh", RiskSafe, "cat", []string{"~/.ssh/id_ed25519.pub"}, nil, "ssh", "github"),
		argvWithArgs("ssh.generate-ed25519", "Gerar chave SSH ed25519", "ssh", RiskDestructive, "ssh-keygen", []string{"-t", "ed25519", "-C", "{email}"}, []Argument{requiredArg("email", ArgString)}, dry("ssh-keygen", "-t", "ed25519", "-C", "{email}", "-f", "./id_ed25519.preview"), "ssh"),
		argv("ssh.agent-start", "Iniciar ssh-agent", "ssh", RiskSafe, "ssh-agent", []string{"-s"}, nil, "ssh"),
		argvWithArgs("ssh.add-key", "Adicionar chave ao ssh-agent", "ssh", RiskSensitive, "ssh-add", []string{"{private_key_path}"}, []Argument{requiredArg("private_key_path", ArgPath)}, nil, "ssh"),
		argv("ssh.test-github", "Testar SSH no GitHub", "ssh", RiskSafe, "ssh", []string{"-T", "git@github.com"}, nil, "ssh", "github"),
		native("ssh.fix-permissions", "Corrigir permissoes de ~/.ssh", "ssh", RiskDestructive, HandlerSSHFixPermissions, "Aplica chmod seguro em ~/.ssh e chaves locais.", "ssh"),
		nativeWithArgs("ssh.pem-permissions", "Corrigir permissao de PEM", "ssh", RiskDestructive, HandlerSSHPemPermissions, []Argument{requiredArg("pem_path", ArgPath)}, "Ajusta permissao 0400 de arquivo PEM.", "ssh"),
		argvWithArgs("ssh.connect-pem", "Conectar com chave PEM", "ssh", RiskSafe, "ssh", []string{"-i", "{pem_path}", "{user}@{host}"}, []Argument{requiredArg("pem_path", ArgPath), requiredArg("user", ArgString), requiredArg("host", ArgHost)}, nil, "ssh"),
		nativeWithArgs("ssh.scp-to-remote", "Copiar arquivo para remoto", "ssh", RiskSensitive, HandlerSCPToRemote, []Argument{requiredArg("local_path", ArgPath), requiredArg("user", ArgString), requiredArg("host", ArgHost), requiredArg("remote_path", ArgPath)}, "Copia arquivo via handler seguro.", "ssh"),
		nativeWithArgs("ssh.scp-from-remote", "Copiar arquivo do remoto", "ssh", RiskSensitive, HandlerSCPFromRemote, []Argument{requiredArg("user", ArgString), requiredArg("host", ArgHost), requiredArg("remote_path", ArgPath), requiredArg("local_path", ArgPath)}, "Copia arquivo via handler seguro.", "ssh"),
		nativeWithArgs("ssh.forward", "Criar tunel SSH local", "ssh", RiskSafe, HandlerSSHForward, []Argument{requiredArg("host", ArgHost), requiredArg("service_or_ports", ArgString)}, "Cria forward SSH com bind local seguro.", "ssh", "tailscale"),
		native("ssh.forward-list", "Listar forwards SSH", "ssh", RiskSafe, HandlerSSHForwardList, "Lista sessoes de forward gerenciadas.", "ssh"),
		nativeWithArgs("ssh.forward-stop", "Parar forward SSH", "ssh", RiskDestructive, HandlerSSHForwardStop, []Argument{requiredArg("name", ArgServiceName)}, "Para forward gerenciado pelo PocketCli.", "ssh"),

		argv("git.status", "Mostrar status Git", "git", RiskSafe, "git", []string{"status"}, nil, "git"),
		argv("git.add-all", "Adicionar todos arquivos ao index", "git", RiskDestructive, "git", []string{"add", "."}, dry("git", "status", "--short"), "git"),
		argvWithArgs("git.commit", "Criar commit", "git", RiskDestructive, "git", []string{"commit", "-m", "{message}"}, []Argument{requiredArg("message", ArgString)}, dry("git", "diff", "--cached", "--stat"), "git"),
		argv("git.push", "Enviar branch atual", "git", RiskDestructive, "git", []string{"push"}, dry("git", "status", "-sb"), "git"),
		argv("git.pull", "Atualizar branch atual", "git", RiskDestructive, "git", []string{"pull"}, dry("git", "status", "-sb"), "git"),
		argv("git.fetch-prune", "Fetch com prune", "git", RiskSafe, "git", []string{"fetch", "--all", "--prune"}, nil, "git"),
		argv("git.remote-show", "Mostrar remotes", "git", RiskSafe, "git", []string{"remote", "-v"}, nil, "git"),
		argvWithArgs("git.remote-to-ssh", "Trocar origin para SSH", "git", RiskDestructive, "git", []string{"remote", "set-url", "origin", "git@github.com:{owner}/{repo}.git"}, []Argument{requiredArg("owner", ArgString), requiredArg("repo", ArgString)}, dry("git", "remote", "-v"), "git", "github"),
		argvWithArgs("git.remote-to-https", "Trocar origin para HTTPS", "git", RiskDestructive, "git", []string{"remote", "set-url", "origin", "https://github.com/{owner}/{repo}.git"}, []Argument{requiredArg("owner", ArgString), requiredArg("repo", ArgString)}, dry("git", "remote", "-v"), "git", "github"),
		argv("git.set-main", "Renomear branch atual para main", "git", RiskDestructive, "git", []string{"branch", "-M", "main"}, dry("git", "branch", "--show-current"), "git"),
		argv("git.push-upstream-main", "Enviar main com upstream", "git", RiskDestructive, "git", []string{"push", "-u", "origin", "main"}, dry("git", "branch", "--show-current"), "git"),
		argv("git.branches", "Listar branches", "git", RiskSafe, "git", []string{"branch", "-a"}, nil, "git"),
		argv("git.graph", "Mostrar grafo Git", "git", RiskSafe, "git", []string{"log", "--oneline", "--graph", "--decorate", "--all"}, nil, "git"),
		argv("git.gone", "Listar branches gone", "git", RiskSafe, "git", []string{"branch", "-vv"}, nil, "git"),
		native("git.clean-local-gone", "Remover branches locais gone", "git", RiskDestructive, HandlerGitCleanLocalGone, "Planeja limpeza de branches gone; execucao real fica bloqueada no MVP.", "git"),
		native("git.force-clean-local-gone", "Forcar remocao de branches gone", "git", RiskDestructive, HandlerGitForceCleanGone, "Planeja limpeza forcada; execucao real fica bloqueada no MVP.", "git"),
		native("git.clean-merged", "Remover branches mergeadas", "git", RiskDestructive, HandlerGitCleanMerged, "Planeja limpeza de branches mergeadas; execucao real fica bloqueada no MVP.", "git"),
		argv("git.reflog", "Mostrar reflog", "git", RiskSafe, "git", []string{"reflog"}, nil, "git"),
		argv("git.fsck", "Verificar objetos Git", "git", RiskSafe, "git", []string{"fsck", "--full"}, nil, "git"),
		argv("git.reset-hard", "Reset hard para HEAD", "git", RiskDestructive, "git", []string{"reset", "--hard", "HEAD"}, dry("git", "status", "--short"), "git"),
		argv("git.clean-files", "Remover arquivos nao rastreados", "git", RiskDestructive, "git", []string{"clean", "-fd"}, dry("git", "clean", "-fdn"), "git"),
		argvWithArgs("git.clone-ssh", "Clonar repo via SSH", "git", RiskSafe, "git", []string{"clone", "git@github.com:{owner}/{repo}.git"}, []Argument{requiredArg("owner", ArgString), requiredArg("repo", ArgString)}, nil, "git", "github"),
		argvWithArgs("git.copy-without-git", "Copiar pasta sem .git", "git", RiskSafe, "rsync", []string{"-a", "--exclude=.git", "{source_dir}/", "{target_dir}/"}, []Argument{requiredArg("source_dir", ArgPath), requiredArg("target_dir", ArgPath)}, nil, "git", "fs"),

		argvWithArgs("port.check", "Verificar porta TCP local", "port", RiskSafe, "lsof", []string{"-nP", "-iTCP:{port}", "-sTCP:LISTEN"}, []Argument{requiredArg("port", ArgPort)}, nil, "port"),
		nativeWithArgs("port.kill", "Encerrar processo em porta", "port", RiskDestructive, HandlerPortKill, []Argument{requiredArg("port", ArgPort)}, "Planeja e encerra processo dono da porta com revalidacao.", "port"),
		argv("ports.list", "Listar portas TCP escutando", "port", RiskSensitive, "lsof", []string{"-iTCP", "-sTCP:LISTEN", "-n", "-P"}, nil, "port"),
		argvWithArgs("process.find", "Buscar processo", "process", RiskSensitive, "pgrep", []string{"-af", "{name}"}, []Argument{requiredArg("name", ArgString)}, nil, "process"),
		nativeWithArgs("process.kill", "Encerrar processo por PID", "process", RiskDestructive, HandlerProcessKill, []Argument{requiredArg("pid", ArgInt)}, "Encerra PID com protecoes.", "process"),
		nativeWithArgs("process.kill-force", "Forcar encerramento por PID", "process", RiskDestructive, HandlerProcessKill, []Argument{requiredArg("pid", ArgInt)}, "Forca encerramento com --force.", "process"),
		argv("process.top", "Abrir top", "process", RiskSafe, "top", []string{}, nil, "process"),

		argv("fs.pwd", "Mostrar diretorio atual", "fs", RiskSafe, "pwd", []string{}, nil, "fs"),
		argv("fs.list", "Listar arquivos", "fs", RiskSensitive, "ls", []string{"-la"}, nil, "fs"),
		argv("fs.disk", "Mostrar disco", "fs", RiskSafe, "df", []string{"-h"}, nil, "fs"),
		argv("fs.size-current", "Tamanho do diretorio atual", "fs", RiskSensitive, "du", []string{"-sh", "."}, nil, "fs"),
		argvWithArgs("fs.size-path", "Tamanho de path", "fs", RiskSensitive, "du", []string{"-sh", "{path}"}, []Argument{requiredArg("path", ArgPath)}, nil, "fs"),
		argvWithArgs("fs.find", "Buscar arquivo por nome", "fs", RiskSensitive, "find", []string{"{base_path}", "-name", "{name}"}, []Argument{requiredArg("base_path", ArgPath), requiredArg("name", ArgString)}, nil, "fs"),
		argvWithArgs("fs.find-git", "Buscar repos Git", "fs", RiskSensitive, "find", []string{"{base_path}", "-maxdepth", "{depth}", "-type", "d", "-name", ".git"}, []Argument{requiredArg("base_path", ArgPath), optionalArg("depth", ArgInt, "3")}, nil, "fs", "git"),
		argvWithArgs("fs.recent-dirs", "Diretorios recentes", "fs", RiskSensitive, "find", []string{"{base_path}", "-maxdepth", "{depth}", "-type", "d", "-mtime", "-7"}, []Argument{requiredArg("base_path", ArgPath), optionalArg("depth", ArgInt, "3")}, nil, "fs"),
		argvWithArgs("fs.tree", "Mostrar arvore de arquivos", "fs", RiskSensitive, "tree", []string{"-L", "{depth}"}, []Argument{optionalArg("depth", ArgInt, "2")}, nil, "fs"),
		nativeWithArgs("fs.tar-gz", "Criar arquivo tar.gz", "fs", RiskDestructive, HandlerFsTarGz, []Argument{requiredArg("archive_name", ArgPath), requiredArg("source_path", ArgPath)}, "Cria arquivo compactado com validacao de path.", "fs"),
		nativeWithArgs("fs.unzip", "Extrair zip", "fs", RiskDestructive, HandlerFsUnzip, []Argument{requiredArg("zip_path", ArgPath)}, "Extrai zip com validacao de path.", "fs"),

		nativeWithArgs("env.generate-secret-hex", "Gerar secret hexadecimal", "env", RiskSensitive, HandlerEnvGenerateSecretHex, []Argument{requiredArg("name", ArgString)}, "Gera secret efemero e imprime export.", "env", "secret"),
		nativeWithArgs("env.show", "Mostrar variavel de ambiente", "env", RiskSensitive, HandlerEnvShow, []Argument{requiredArg("name", ArgString)}, "Mostra variavel mascarada por padrao.", "env"),
		nativeWithArgs("env.print", "Mostrar variavel com politica de mascara", "env", RiskSensitive, HandlerEnvShow, []Argument{requiredArg("name", ArgString)}, "Alias seguro para env.show.", "env"),
		native("env.list", "Listar ambiente mascarado", "env", RiskSensitive, HandlerEnvList, "Lista variaveis mascaradas; reveal em massa bloqueado.", "env"),
		nativeWithArgs("env.export", "Orientar export de variavel", "env", RiskSensitive, HandlerEnvExport, []Argument{requiredArg("name", ArgString)}, "Mostra orientacao sem persistir segredo.", "env"),

		argv("docker.ps", "Listar containers", "docker", RiskSafe, "docker", []string{"ps"}, nil, "docker"),
		argv("docker.ps-all", "Listar todos containers", "docker", RiskSafe, "docker", []string{"ps", "-a"}, nil, "docker"),
		argvWithArgs("docker.logs", "Acompanhar logs de container", "docker", RiskSensitive, "docker", []string{"logs", "-f", "{container}"}, []Argument{requiredArg("container", ArgString)}, nil, "docker", "logs"),
		argvWithArgs("docker.shell-sh", "Abrir sh em container", "docker", RiskSafe, "docker", []string{"exec", "-it", "{container}", "sh"}, []Argument{requiredArg("container", ArgString)}, nil, "docker"),
		argvWithArgs("docker.shell-bash", "Abrir bash em container", "docker", RiskSafe, "docker", []string{"exec", "-it", "{container}", "bash"}, []Argument{requiredArg("container", ArgString)}, nil, "docker"),
		argv("docker.compose-up", "Subir compose", "docker", RiskDestructive, "docker", []string{"compose", "up", "-d"}, dry("docker", "compose", "config", "--quiet"), "docker"),
		argv("docker.compose-down", "Parar compose", "docker", RiskDestructive, "docker", []string{"compose", "down"}, dry("docker", "compose", "ps"), "docker"),
		argv("docker.compose-logs", "Logs do compose", "docker", RiskSensitive, "docker", []string{"compose", "logs", "-f"}, nil, "docker", "logs"),
		argv("docker.system-df", "Uso de disco Docker", "docker", RiskSafe, "docker", []string{"system", "df"}, nil, "docker"),
		argv("docker.prune", "Limpar recursos Docker", "docker", RiskDestructive, "docker", []string{"system", "prune"}, dry("docker", "system", "df"), "docker"),
		argv("docker.volumes", "Listar volumes Docker", "docker", RiskSafe, "docker", []string{"volume", "ls"}, nil, "docker"),
		argv("docker.networks", "Listar redes Docker", "docker", RiskSafe, "docker", []string{"network", "ls"}, nil, "docker"),

		argv("k8s.namespaces", "Listar namespaces", "k8s", RiskSafe, "kubectl", []string{"get", "ns"}, nil, "k8s"),
		argv("k8s.pods-all", "Listar pods todos namespaces", "k8s", RiskSafe, "kubectl", []string{"get", "pods", "-A"}, nil, "k8s"),
		argvWithArgs("k8s.pods", "Listar pods do namespace", "k8s", RiskSafe, "kubectl", []string{"get", "pods", "-n", "{namespace}"}, []Argument{requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.describe-pod", "Descrever pod", "k8s", RiskSafe, "kubectl", []string{"describe", "pod", "{pod}", "-n", "{namespace}"}, []Argument{requiredArg("pod", ArgString), requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.logs-pod", "Logs de pod", "k8s", RiskSensitive, "kubectl", []string{"logs", "-f", "{pod}", "-n", "{namespace}"}, []Argument{requiredArg("pod", ArgString), requiredArg("namespace", ArgString)}, nil, "k8s", "logs"),
		argvWithArgs("k8s.logs-target", "Logs de target Kubernetes", "k8s", RiskSensitive, "kubectl", []string{"logs", "-f", "{target}", "-n", "{namespace}"}, []Argument{requiredArg("target", ArgString), requiredArg("namespace", ArgString)}, nil, "k8s", "logs"),
		argvWithArgs("k8s.shell", "Abrir shell em pod", "k8s", RiskSafe, "kubectl", []string{"exec", "-it", "{pod}", "-n", "{namespace}", "--", "sh"}, []Argument{requiredArg("pod", ArgString), requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.services", "Listar services", "k8s", RiskSafe, "kubectl", []string{"get", "svc", "-n", "{namespace}"}, []Argument{requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.ingress", "Listar ingress", "k8s", RiskSafe, "kubectl", []string{"get", "ingress", "-n", "{namespace}"}, []Argument{requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.restart-deployment", "Reiniciar deployment", "k8s", RiskDestructive, "kubectl", []string{"rollout", "restart", "deployment/{deployment}", "-n", "{namespace}"}, []Argument{requiredArg("deployment", ArgString), requiredArg("namespace", ArgString)}, dry("kubectl", "rollout", "status", "deployment/{deployment}", "-n", "{namespace}"), "k8s"),
		argvWithArgs("k8s.rollout-status", "Status de rollout", "k8s", RiskSafe, "kubectl", []string{"rollout", "status", "deployment/{deployment}", "-n", "{namespace}"}, []Argument{requiredArg("deployment", ArgString), requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.port-forward-service", "Port-forward de service", "k8s", RiskSafe, "kubectl", []string{"port-forward", "svc/{service}", "{local_port}:{remote_port}", "-n", "{namespace}"}, []Argument{requiredArg("service", ArgString), requiredArg("local_port", ArgPort), requiredArg("remote_port", ArgPort), requiredArg("namespace", ArgString)}, nil, "k8s"),
		argvWithArgs("k8s.top-pods", "Top pods", "k8s", RiskSafe, "kubectl", []string{"top", "pods", "-n", "{namespace}"}, []Argument{requiredArg("namespace", ArgString)}, nil, "k8s"),
		argv("k8s.top-nodes", "Top nodes", "k8s", RiskSafe, "kubectl", []string{"top", "nodes"}, nil, "k8s"),

		argvWithArgs("keycloak.logs", "Logs do Keycloak", "keycloak", RiskSensitive, "kubectl", []string{"logs", "-f", "deployment/keycloak", "-n", "{namespace}"}, []Argument{requiredArg("namespace", ArgString)}, nil, "keycloak", "logs"),
		argvWithArgs("keycloak.port-forward", "Port-forward Keycloak", "keycloak", RiskSafe, "kubectl", []string{"port-forward", "svc/keycloak", "{local_port}:8080", "-n", "{namespace}"}, []Argument{requiredArg("local_port", ArgPort), requiredArg("namespace", ArgString)}, nil, "keycloak"),
		Recipe{ID: "keycloak.kcadm-login", Title: "Login kcadm seguro", Category: "keycloak", Risk: RiskSensitive, Kind: KindInfoOnly, Source: SourceBuiltin, Description: "Use prompt seguro ou env_reference; senha bruta por CLI e bloqueada.", Tags: []string{"keycloak"}},
		argvWithArgs("keycloak.users", "Listar usuarios Keycloak", "keycloak", RiskSensitive, "./kcadm.sh", []string{"get", "users", "-r", "{realm}"}, []Argument{requiredArg("realm", ArgString)}, nil, "keycloak"),
		argvWithArgs("keycloak.groups", "Listar grupos Keycloak", "keycloak", RiskSensitive, "./kcadm.sh", []string{"get", "groups", "-r", "{realm}"}, []Argument{requiredArg("realm", ArgString)}, nil, "keycloak"),
		argvWithArgs("keycloak.find-user-email", "Buscar usuario por email", "keycloak", RiskSensitive, "./kcadm.sh", []string{"get", "users", "-r", "{realm}", "-q", "email={email}"}, []Argument{requiredArg("realm", ArgString), requiredArg("email", ArgString)}, nil, "keycloak"),

		argv("mac.hidden-on", "Mostrar arquivos ocultos", "mac", RiskDestructive, "defaults", []string{"write", "com.apple.finder", "AppleShowAllFiles", "-bool", "true"}, dry("defaults", "read", "com.apple.finder", "AppleShowAllFiles"), "mac"),
		argv("mac.hidden-off", "Ocultar arquivos ocultos", "mac", RiskDestructive, "defaults", []string{"write", "com.apple.finder", "AppleShowAllFiles", "-bool", "false"}, dry("defaults", "read", "com.apple.finder", "AppleShowAllFiles"), "mac"),
		argv("mac.arch", "Arquitetura macOS", "mac", RiskSafe, "uname", []string{"-m"}, nil, "mac"),
		argv("mac.version", "Versao macOS", "mac", RiskSafe, "sw_vers", []string{}, nil, "mac"),
		argv("brew.list", "Listar pacotes Brew", "brew", RiskSafe, "brew", []string{"list"}, nil, "brew"),
		argv("brew.update-upgrade", "Atualizar Brew", "brew", RiskDestructive, "brew", []string{"upgrade"}, dry("brew", "outdated"), "brew"),
		argv("brew.services", "Listar servicos Brew", "brew", RiskSafe, "brew", []string{"services", "list"}, nil, "brew"),

		argv("tailscale.status", "Status Tailscale", "tailscale", RiskSensitive, "tailscale", []string{"status", "--json"}, nil, "tailscale"),
		argv("tailscale.hosts", "Hosts Tailscale", "tailscale", RiskSensitive, "tailscale", []string{"status", "--json"}, nil, "tailscale"),
		argv("tailscale.ip", "IPs Tailscale", "tailscale", RiskSensitive, "tailscale", []string{"ip"}, nil, "tailscale"),
		argvWithArgs("tailscale.ping", "Ping Tailscale", "tailscale", RiskSafe, "tailscale", []string{"ping", "{host}"}, []Argument{requiredArg("host", ArgHost)}, nil, "tailscale"),
		argv("tailscale.up", "Subir Tailscale", "tailscale", RiskDestructive, "tailscale", []string{"up"}, dry("tailscale", "status", "--json"), "tailscale"),
		argv("tailscale.down", "Derrubar Tailscale", "tailscale", RiskDestructive, "tailscale", []string{"down"}, dry("tailscale", "status", "--json"), "tailscale"),

		argvWithArgs("tmux.new", "Criar sessao tmux", "tmux", RiskSafe, "tmux", []string{"new", "-s", "{name}"}, []Argument{requiredArg("name", ArgServiceName)}, nil, "tmux"),
		argvWithArgs("tmux.attach", "Anexar tmux", "tmux", RiskSafe, "tmux", []string{"attach", "-t", "{name}"}, []Argument{requiredArg("name", ArgServiceName)}, nil, "tmux"),
		argv("tmux.list", "Listar tmux", "tmux", RiskSafe, "tmux", []string{"ls"}, nil, "tmux"),
		argvWithArgs("tmux.kill", "Matar sessao tmux", "tmux", RiskDestructive, "tmux", []string{"kill-session", "-t", "{name}"}, []Argument{requiredArg("name", ArgServiceName)}, dry("tmux", "ls"), "tmux"),
	}
	return recipes
}

func argv(id, title, category string, risk Risk, executable string, args []string, dryRun *Template, tags ...string) Recipe {
	return argvWithArgs(id, title, category, risk, executable, args, nil, dryRun, tags...)
}

func argvWithArgs(id, title, category string, risk Risk, executable string, args []string, recipeArgs []Argument, dryRun *Template, tags ...string) Recipe {
	return Recipe{
		ID:                 id,
		Title:              title,
		Category:           category,
		Risk:               risk,
		Kind:               KindArgvTemplate,
		Source:             SourceBuiltin,
		Description:        title + ".",
		Args:               recipeArgs,
		ArgvTemplate:       &Template{Executable: executable, Args: args},
		DryRunArgvTemplate: dryRun,
		Dependencies:       binaryDependency(executable),
		Examples:           []string{"pocket catalogue show " + id},
		Tags:               tags,
	}
}

func native(id, title, category string, risk Risk, handler NativeHandlerID, description string, tags ...string) Recipe {
	return nativeWithArgs(id, title, category, risk, handler, nil, description, tags...)
}

func nativeWithArgs(id, title, category string, risk Risk, handler NativeHandlerID, args []Argument, description string, tags ...string) Recipe {
	return Recipe{
		ID:          id,
		Title:       title,
		Category:    category,
		Risk:        risk,
		Kind:        KindNativeHandler,
		Source:      SourceBuiltin,
		Description: description,
		Args:        args,
		Handler:     handler,
		Examples:    []string{"pocket catalogue show " + id},
		Tags:        tags,
	}
}

func requiredArg(name string, typ ArgType) Argument {
	return Argument{Name: name, Type: typ, Required: true}
}

func optionalArg(name string, typ ArgType, defaultValue string) Argument {
	return Argument{Name: name, Type: typ, Required: false, Default: defaultValue}
}

func dry(executable string, args ...string) *Template {
	return &Template{Executable: executable, Args: args}
}

func binaryDependency(name string) []Dependency {
	if name == "" || name == "." || name[0] == '/' {
		return nil
	}
	return []Dependency{{
		Name:     name,
		Type:     "binary",
		Required: true,
		Check: DependencyCheck{
			Kind:  "binary_exists",
			Value: name,
		},
	}}
}
