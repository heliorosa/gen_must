package testpkg

func DoStuff[T any]() (T, error) {
	//@gen_must
	var zero T
	return zero, nil
}
