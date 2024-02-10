package mustgen

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func filePath(name string) string { return filepath.Join("testdata", "testpkg", name) }

func goFilePath(idx int) string { return filePath(fmt.Sprintf("testpkg_%d.go", idx)) }

func expectedFilePath(idx int) string { return goFilePath(idx) + ".expected" }

func TestMustGen(t *testing.T) {
	const testCount = 9
	for i := 0; i < testCount; i++ {
		goFile := goFilePath(i)
		t.Run(fmt.Sprintf("File: %s", goFile), func(t *testing.T) {
			pkg, err := ParsePackage([]string{goFile})
			require.NoError(t, err)
			buffer := bytes.NewBuffer(make([]byte, 0, 1024))
			err = Generate(buffer, pkg)
			require.NoError(t, err)
			fmtCode := bytes.NewBuffer(make([]byte, 0, 1024))
			err = GoFmt(buffer, fmtCode)
			require.NoError(t, err)
			exp, err := os.ReadFile(expectedFilePath(i))
			require.NoError(t, err)
			require.Equal(t, exp, fmtCode.Bytes())
		})
	}
}
