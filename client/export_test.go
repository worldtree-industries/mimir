package client

// LocalConfigFile exposes where Local Values are set, relative to the
// repository root, to the external tests.
const LocalConfigFile = localConfigFile

// WithWorkingDir sets the directory the search for the repository root
// begins in, standing in for the process's working directory. It exists
// only for tests, so production code can't point the client at another
// local config file.
func WithWorkingDir(dir string) Option {
	return func(o *options) { o.workDir = dir }
}
