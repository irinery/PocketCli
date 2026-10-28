module pocketcli

go 1.22

require (
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/spf13/cobra v1.8.1
)

replace github.com/spf13/cobra => ./third_party/cobra

replace github.com/charmbracelet/bubbletea => ./third_party/bubbletea
