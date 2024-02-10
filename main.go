package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

func showError(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(-1)
}

var (
	errNoPackageFound   = errors.New("no package found")
	errUnknownFieldType = errors.New("unknown field type")
	errNoReturnValues   = errors.New("no return values")
	errNoErrorReturn    = errors.New("no error returned")
)

type errFunctionNotFound string

func (e errFunctionNotFound) Error() string {
	return fmt.Sprintf("function not found: %s", string(e))
}

func parsePackage(patterns []string) (*packages.Package, error) {
	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedName |
				packages.NeedFiles |
				packages.NeedCompiledGoFiles |
				packages.NeedTypes |
				packages.NeedSyntax |
				packages.NeedTypesInfo,
			Tests: false,
		},
		patterns...,
	)
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, errNoPackageFound
	}
	return pkgs[0], nil
}

func goFormat(src io.Reader, dst io.Writer) error {
	b, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	b, err = format.Source(b)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, bytes.NewReader(b))
	return err
}

func isDirectory(name string) (bool, error) {
	info, err := os.Stat(name)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func walkCode(pkg *packages.Package, tagComment string, genFn func(newName string, fnDecl *ast.FuncDecl) error) error {
	for _, file := range pkg.Syntax {
		var err error
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			var firstComment *ast.Comment
		Outer:
			for _, i := range file.Comments {
				for _, j := range i.List {
					if j.Pos() >= fn.Body.Lbrace && j.Pos() <= fn.Body.Rbrace {
						firstComment = j
						break Outer
					}
				}
			}
			if firstComment == nil {
				return true
			}
			var firstNode ast.Node
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if firstNode != nil {
					return false
				}
				if n == nil || n == fn.Body {
					return true
				}
				firstNode = n
				return false
			})
			if firstNode.Pos() < firstComment.Pos() {
				return true
			}
			pref := "//" + tagComment
			if !strings.HasPrefix(firstComment.Text, pref) {
				return true
			}
			newName := strings.TrimPrefix(firstComment.Text, pref)
			if strings.HasPrefix(newName, ":") {
				newName = strings.TrimSpace(newName[1:])
			} else if newName == "" {
				newName = mustName(fn.Name.Name)
			}
			if err = genFn(newName, fn); err != nil {
				return false
			}
			return true
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func generateType(typ ast.Expr) (string, error) {
	switch t := typ.(type) {
	case *ast.StarExpr:
		tx, err := generateType(t.X)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("*%s", tx), nil
	case *ast.Ident:
		return t.Name, nil
	case *ast.Ellipsis:
		return fmt.Sprintf("...%s", t.Elt), nil
	case *ast.BinaryExpr:
		if !t.Op.IsOperator() {
			return "", errUnknownFieldType
		}
		tx, err := generateType(t.X)
		if err != nil {
			return "", err
		}
		ty, err := generateType(t.Y)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", tx, t.Op.String(), ty), nil
	case *ast.UnaryExpr:
		return fmt.Sprintf("%s%s", t.Op.String(), t.X), nil
	case *ast.IndexExpr:
		ident, err := generateType(t.X)
		if err != nil {
			return "", err
		}
		expr, err := generateType(t.Index)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s[%s]", ident, expr), nil
	case *ast.IndexListExpr:
		ident, err := generateType(t.X)
		if err != nil {
			return "", err
		}
		exprs := make([]string, 0, len(t.Indices))
		for _, i := range t.Indices {
			e, err := generateType(i)
			if err != nil {
				return "", err
			}
			exprs = append(exprs, e)
		}
		return fmt.Sprintf("%s[%s]", ident, strings.Join(exprs, ",")), nil
	default:
		return "", errUnknownFieldType
	}
}

func generateReceiver(recv *ast.FieldList) (name string, decl string, err error) {
	if recv == nil {
		return "", "", err
	}
	name = recv.List[0].Names[0].Name
	decl, err = generateType(recv.List[0].Type)
	if err != nil {
		return "", "", err
	}
	if name == "_" {
		name = "t"
	}
	return fmt.Sprintf("(%s %s)", name, decl), name + ".", nil
}

func generateParams(params *ast.FieldList) (decl string, use string, err error) {
	if params == nil || len(params.List) == 0 {
		return "", "", nil
	}
	names := make([]string, 0, len(params.List))
	types := make([]string, 0, len(params.List))
	for _, i := range params.List {
		names = append(names, i.Names[0].Name)
		t, err := generateType(i.Type)
		if err != nil {
			return "", "", err
		}
		types = append(types, fmt.Sprintf("%s %s", i.Names[0].Name, t))
	}
	return strings.Join(types, ","), strings.Join(names, ","), nil
}

func generateReturns(rets *ast.FieldList) (decl []string, use []string, err error) {
	if rets == nil || len(rets.List) == 0 {
		return nil, nil, errNoReturnValues
	}
	names := make([]string, 0, len(rets.List))
	types := make([]string, 0, len(rets.List))
	for i, ret := range rets.List {
		names = append(names, fmt.Sprintf("var%d", i))
		t, err := generateType(ret.Type)
		if err != nil {
			return nil, nil, err
		}
		types = append(types, t)
	}
	if types[len(types)-1] != "error" {
		return nil, nil, errNoErrorReturn
	}
	names[len(names)-1] = "err"
	return types, names, nil
}

func generateTypeParams(typeParams *ast.FieldList) (decl string, use string, err error) {
	if typeParams == nil || len(typeParams.List) == 0 {
		return "", "", nil
	}
	names := make([]string, 0, len(typeParams.List))
	types := make([]string, 0, len(typeParams.List))
	_ = types
	for _, i := range typeParams.List {
		names = append(names, i.Names[0].Name)
		t, err := generateType(i.Type)
		if err != nil {
			return "", "", err
		}
		types = append(types, fmt.Sprintf("%s %s", i.Names[0].Name, t))
	}
	if len(names) > 0 {
		use = fmt.Sprintf("[%s]", strings.Join(names, ","))
		decl = fmt.Sprintf("[%s]", strings.Join(types, ","))
	}
	return decl, use, nil
}

func mustName(name string) string {
	f := name[:1]
	if strings.ToUpper(f) == f {
		return "Must" + name
	}
	return "must" + strings.ToUpper(f) + name[1:]
}

type generator struct{ *bytes.Buffer }

func newGenerator() *generator { return &generator{bytes.NewBuffer(make([]byte, 0, 1024))} }

func (g *generator) generateHead(pkgName string) {
	fmt.Fprintf(g.Buffer, "// Code generated - DO NOT EDIT.\n// This file is auto generated by gen_must and any manual changes will be lost.\n\n")
	fmt.Fprintf(g.Buffer, "package %s\n\n", pkgName)
}

func (g *generator) generateMust(newName string, fnDecl *ast.FuncDecl) error {
	typeParamsDecl, typeParamsUse, err := generateTypeParams(fnDecl.Type.TypeParams)
	if err != nil {
		showError(err)
	}
	recvDecl, recvUse, err := generateReceiver(fnDecl.Recv)
	if err != nil {
		showError(err)
	}
	paramsDecl, paramsUse, err := generateParams(fnDecl.Type.Params)
	if err != nil {
		showError(err)
	}
	retsDecl, retsVars, err := generateReturns(fnDecl.Type.Results)
	if err != nil {
		showError(err)
	}
	fmt.Fprintf(g.Buffer, "// %s has the behavior of %s, except it panics any error\n",
		newName,
		fnDecl.Name,
	)
	fmt.Fprintf(g.Buffer, "func %s %s%s(%s) (%s) {\n",
		recvDecl,
		newName,
		typeParamsDecl,
		paramsDecl,
		strings.Join(retsDecl[:len(retsDecl)-1], ","),
	)
	fmt.Fprintf(g.Buffer, "%s := %s%s%s(%s)\nif err!=nil{panic(err)}\n",
		strings.Join(retsVars, ","),
		recvUse,
		fnDecl.Name,
		typeParamsUse,
		paramsUse,
	)
	rv := retsVars[:len(retsVars)-1]
	if len(rv) > 0 {
		fmt.Fprintf(g.Buffer, "return %s", strings.Join(rv, ","))
	}
	fmt.Fprintf(g.Buffer, "}\n\n")
	return nil
}

func main() {
	var outFile string
	flag.StringVar(&outFile, "out", "-", "output file. default is stdout")
	flag.Parse()
	args := flag.Args()
	pkg, err := parsePackage(args)
	if err != nil {
		showError(err)
	}
	gen := newGenerator()
	gen.generateHead(pkg.Name)
	if err = walkCode(pkg, "@gen_must", gen.generateMust); err != nil {
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
	if err = goFormat(gen, fOut); err != nil {
		showError(err)
	}
}
