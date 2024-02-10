package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/heliorosa/gen_must/mustgen"
)

func showError(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(-1)
}

func isDirectory(name string) (bool, error) {
	info, err := os.Stat(name)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func main() {
	var outFile string
	flag.StringVar(&outFile, "out", "-", "output file. default is stdout")
	flag.Parse()
	args := flag.Args()
	pkg, err := mustgen.ParsePackage(args)
	if err != nil {
		showError(err)
	}
	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	if err = mustgen.Generate(buffer, pkg); err != nil {
		showError(err)
	}
	var fOut io.Writer
	if outFile == "" || outFile == "-" {
		fOut = os.Stdout
	} else {
		var outFileDir string
		isDir, err := isDirectory(args[0])
		if err != nil {
			showError(err)
		}
		if len(args) == 1 && isDir {
			outFileDir = args[0]
		} else {
			outFileDir = filepath.Dir(args[0])
		}
		f, err := os.Create(filepath.Join(outFileDir, outFile))
		if err != nil {
			showError(err)
		}
		defer f.Close()
		fOut = f
	}
	if err = mustgen.GoFmt(buffer, fOut); err != nil {
		showError(err)
	}
}
