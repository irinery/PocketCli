module pocketcli

go 1.22

require (
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/spf13/cobra v1.8.1
	golang.org/x/term v0.28.0
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/sys v0.29.0 // indirect

replace github.com/spf13/cobra => ./third_party/cobra

replace github.com/charmbracelet/bubbletea => ./third_party/bubbletea
