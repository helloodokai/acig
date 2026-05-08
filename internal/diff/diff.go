package diff

type FileDiff struct {
	Path      string
	Added     []string
	Removed   []string
	Patch     string
	IsNew     bool
	IsDelete  bool
	DiffLines map[int]bool // new-file line numbers visible in this diff's hunks
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