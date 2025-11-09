package dirextractor

import (
	"errors"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type extractor struct {
	root string
	fns  []func(path string) error
}

var (
	Skip    = errors.New("skip this file")
	SkipDir = errors.New("skip this directory")
)

func WithExtensions(ext []string) func(*extractor) {
	extFilter := func(path string) error {
		if slices.Contains(ext, filepath.Ext(path)) {
			return nil
		}

		return Skip
	}

	return func(e *extractor) {
		e.fns = append(e.fns, extFilter)
	}
}

func WithIgnoreHidden() func(*extractor) {
	f := func(path string) error {
		components := strings.Split(path, string(os.PathSeparator))

		for _, c := range components[:max(0, len(components)-2)] {
			if strings.HasPrefix(c, ".") {
				return SkipDir
			}
		}

		if strings.HasPrefix(components[len(components)-1], ".") {
			return Skip
		}

		return nil
	}

	return func(e *extractor) {
		e.fns = append(e.fns, f)
	}
}

func WithIgnoreRegs(regs ...string) func(*extractor) {
	var regexes []*regexp.Regexp
	for _, reg := range regs {
		r, err := regexp.Compile(reg)
		if err != nil {
			panic(err)
		}
		regexes = append(regexes, r)
	}

	f := func(path string) error {
		for _, r := range regexes {
			if r.MatchString(path) {
				return Skip
			}
		}

		return nil
	}

	return func(e *extractor) {
		e.fns = append(e.fns, f)
	}
}

func New(root string, opt ...func(*extractor)) extractor {
	ext := extractor{
		root: root,
		fns:  []func(path string) error{},
	}

	for _, opt := range opt {
		opt(&ext)
	}

	return ext
}

func (e extractor) filter(path string) error {
	for _, f := range e.fns {
		if err := f(path); err != nil {
			return err
		}
	}

	return nil
}

func (e extractor) Files() iter.Seq[string] {
	return func(yield func(string) bool) {
		err := filepath.WalkDir(e.root, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				return nil
			}

			abs, err := filepath.Abs(path)
			if err != nil {
				return nil
			}

			switch filter := e.filter(abs); {
			case errors.Is(filter, Skip):
				return nil
			case errors.Is(filter, SkipDir):
				return filepath.SkipDir
			}

			if !yield(abs) {
				return filepath.SkipAll
			}

			return nil
		})

		if err != nil {
			panic(err)
		}
	}
}
