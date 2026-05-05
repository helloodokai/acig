package diff

type FileDiff struct {
	Path     string
	Added    []string
	Removed  []string
	Patch    string
	IsNew    bool
	IsDelete bool
}

type Diff struct {
	Files    []FileDiff
	RawPatch string
	Stats    Stats
}

type Stats struct {
	FilesChanged int
	LinesAdded   int
	LinesRemoved int
}