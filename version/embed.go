package version

import (
	_ "embed"
)

// Logo is the ASCII banner printed by the `version` subcommand.
//
//go:embed logo.txt
var Logo string
