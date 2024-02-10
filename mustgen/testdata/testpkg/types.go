package testpkg

type TypeA struct{}

type TypeB[T any] struct{ T T }

type TypeC[T any, U any] struct {
	T T
	U U
}
