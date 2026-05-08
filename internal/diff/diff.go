package diff

type HunkRange struct {
	Start int
	End   int
}

type FileDiff struct {
	Path       string
	HunkRanges []HunkRange
	StartLine  int
	Added      []string
	Removed    []string
	Patch      string
	IsNew      bool
	IsDelete   bool
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