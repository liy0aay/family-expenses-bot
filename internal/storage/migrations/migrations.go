package migrations

import "embed"

// //go:embed cannot reference files above the package directory,
// so the FS is declared here next to the SQL files.
//
//go:embed *.sql
var FS embed.FS
