package main

import (
	"embed"
	"io/fs"
)

// scaffoldFiles holds the default scaffold that `lclaw init` writes. The
// all: prefix keeps dotfiles such as .containerignore, which embed would
// otherwise skip.
//
//go:embed all:scaffold
var scaffoldFiles embed.FS

// scaffoldFS returns the defaults rooted at the scaffold directory, so the
// paths inside it are the paths lclaw init writes.
func scaffoldFS() fs.FS {
	sub, err := fs.Sub(scaffoldFiles, "scaffold")
	if err != nil {
		panic(err) // the directory is compiled in; this cannot fail
	}
	return sub
}
