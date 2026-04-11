package cliargs

import "strings"

// ReorderForFlagSet moves flags before positionals so Go's flag package can
// parse the more natural CLI shape: command <name> --flag value.
func ReorderForFlagSet(args []string, valueFlags map[string]bool) []string {
	flags := []string{}
	positionals := []string{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		flags = append(flags, arg)
		flagName := strings.TrimLeft(arg, "-")
		if idx := strings.Index(flagName, "="); idx >= 0 {
			flagName = flagName[:idx]
		}
		if valueFlags[flagName] && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	return append(flags, positionals...)
}
