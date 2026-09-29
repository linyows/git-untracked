package main

import (
	"os"

	untracked "github.com/linyows/git-untracked"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(untracked.RunCLI(untracked.Env{
		Out:     os.Stdout,
		Err:     os.Stderr,
		Args:    os.Args[1:],
		Version: version,
		Commit:  commit,
		Date:    date,
	}))
}
