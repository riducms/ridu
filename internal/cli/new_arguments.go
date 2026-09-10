package cli

import (
	"flag"
	"strings"
)

// Project initializers conventionally accept options on either side of the
// directory. Keep flag.FlagSet's value parsing and diagnostics while collecting
// positional arguments separately. An explicit -- still ends option parsing.
func parseNewProjectFlags(flags *flag.FlagSet, args []string) error {
	options := make([]string, 0, len(args))
	var positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(argument, "-") || argument == "-" {
			positional = append(positional, argument)
			continue
		}
		options = append(options, argument)
		name := strings.TrimPrefix(strings.TrimPrefix(argument, "-"), "-")
		if strings.Contains(name, "=") {
			continue
		}
		definition := flags.Lookup(name)
		if definition == nil {
			continue // Let FlagSet report unknown flags, including malformed names.
		}
		boolean, ok := definition.Value.(interface{ IsBoolFlag() bool })
		if ok && boolean.IsBoolFlag() {
			continue
		}
		if index+1 == len(args) {
			return flags.Parse(options) // Preserve the missing-value diagnostic.
		}
		index++
		options = append(options, args[index])
	}
	if len(positional) != 0 {
		options = append(options, "--")
		options = append(options, positional...)
	}
	return flags.Parse(options)
}
