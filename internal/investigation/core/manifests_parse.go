package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type manifestFacts struct {
	Name                   string               `json:"name,omitempty"`
	Services               []string             `json:"services,omitempty"`
	Dependencies           []manifestDependency `json:"dependencies,omitempty"`
	UnresolvedDependencies []string             `json:"unresolved_dependencies,omitempty"`
}

type manifestDependency struct {
	Service   string `json:"service"`
	DependsOn string `json:"depends_on"`
}

func parseManifest(kind string, content []byte) (manifestFacts, error) {
	var facts manifestFacts
	if len(content) > 2*1024*1024 {
		return facts, errors.New("manifesto excede 2 MiB")
	}
	switch kind {
	case "go_module":
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				facts.Name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module ")), `"`)
				break
			}
		}
		if facts.Name == "" {
			return facts, errors.New("go.mod sem module")
		}
	case "node_package":
		var packageJSON struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(content, &packageJSON) != nil {
			// A workspace pnpm é YAML; não infere nome a partir dela.
			if !bytes.Contains(content, []byte("packages:")) {
				return facts, errors.New("package manifest inválido")
			}
			if err := safeYAML(content); err != nil {
				return facts, err
			}
			return facts, nil
		}
		facts.Name = packageJSON.Name
	case "container":
		found := false
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToUpper(line), "FROM ") {
				found = true
				break
			}
		}
		if !found {
			return facts, errors.New("Dockerfile sem FROM")
		}
	case "compose":
		if err := safeYAML(content); err != nil {
			return facts, err
		}
		var doc struct {
			Services map[string]yaml.Node `yaml:"services"`
		}
		if yaml.Unmarshal(content, &doc) != nil || len(doc.Services) == 0 {
			return facts, errors.New("Compose sem services")
		}
		for service := range doc.Services {
			if !runtimeName.MatchString(service) || len(facts.Services) >= 100 {
				return manifestFacts{}, errors.New("nome ou limite de service inválido")
			}
			facts.Services = append(facts.Services, service)
		}
		sort.Strings(facts.Services)
		for _, service := range facts.Services {
			node := doc.Services[service]
			if node.Kind != yaml.MappingNode {
				return manifestFacts{}, errors.New("service Compose inválido")
			}
			for index := 0; index < len(node.Content); index += 2 {
				if node.Content[index].Value != "depends_on" {
					continue
				}
				dependencies := node.Content[index+1]
				var names []string
				switch dependencies.Kind {
				case yaml.SequenceNode:
					for _, item := range dependencies.Content {
						if item.Kind != yaml.ScalarNode {
							return manifestFacts{}, errors.New("depends_on Compose inválido")
						}
						names = append(names, item.Value)
					}
				case yaml.MappingNode:
					for i := 0; i < len(dependencies.Content); i += 2 {
						if dependencies.Content[i].Kind != yaml.ScalarNode {
							return manifestFacts{}, errors.New("depends_on Compose inválido")
						}
						names = append(names, dependencies.Content[i].Value)
					}
				default:
					return manifestFacts{}, errors.New("depends_on Compose inválido")
				}
				seen := make(map[string]bool, len(names))
				for _, dependency := range names {
					if !runtimeName.MatchString(dependency) || dependency == service {
						return manifestFacts{}, errors.New("dependência Compose inválida")
					}
					if seen[dependency] {
						continue
					}
					seen[dependency] = true
					if _, exists := doc.Services[dependency]; !exists {
						facts.UnresolvedDependencies = append(facts.UnresolvedDependencies, service+"->"+dependency)
						continue
					}
					facts.Dependencies = append(facts.Dependencies, manifestDependency{Service: service, DependsOn: dependency})
				}
			}
		}
		sort.Slice(facts.Dependencies, func(i, j int) bool {
			if facts.Dependencies[i].Service != facts.Dependencies[j].Service {
				return facts.Dependencies[i].Service < facts.Dependencies[j].Service
			}
			return facts.Dependencies[i].DependsOn < facts.Dependencies[j].DependsOn
		})
		sort.Strings(facts.UnresolvedDependencies)
	case "api_contract":
		var doc struct {
			OpenAPI string `json:"openapi" yaml:"openapi"`
			Swagger string `json:"swagger" yaml:"swagger"`
			Info    struct {
				Title string `json:"title" yaml:"title"`
			} `json:"info" yaml:"info"`
		}
		if json.Unmarshal(content, &doc) != nil {
			if err := safeYAML(content); err != nil || yaml.Unmarshal(content, &doc) != nil {
				return facts, errors.New("OpenAPI inválido")
			}
		}
		if doc.OpenAPI == "" && doc.Swagger == "" {
			return facts, errors.New("versão OpenAPI ausente")
		}
		facts.Name = doc.Info.Title
	default:
		return facts, errors.New("tipo de manifesto não suportado")
	}
	if len(facts.Name) > 120 || strings.ContainsAny(facts.Name, "\x00\r\n") {
		return manifestFacts{}, errors.New("nome de manifesto inválido")
	}
	return facts, nil
}

func safeYAML(content []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("múltiplos documentos YAML")
	}
	count := 0
	var visit func(*yaml.Node, int) error
	visit = func(node *yaml.Node, depth int) error {
		count++
		if count > 10000 || depth > 64 || node.Kind == yaml.AliasNode {
			return errors.New("YAML complexo ou com alias")
		}
		if node.Tag != "" {
			switch node.Tag {
			case "!!map", "!!seq", "!!str", "!!int", "!!bool", "!!null", "!!float":
			default:
				return errors.New("tag YAML não permitida")
			}
		}
		if node.Kind == yaml.MappingNode {
			seen := make(map[string]bool, len(node.Content)/2)
			for index := 0; index < len(node.Content); index += 2 {
				key := node.Content[index]
				if key.Kind != yaml.ScalarNode || seen[key.Value] {
					return errors.New("chave YAML duplicada ou inválida")
				}
				seen[key.Value] = true
			}
		}
		for _, child := range node.Content {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(&root, 0)
}
