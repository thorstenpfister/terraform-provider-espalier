package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// contextHash hashes what a build reads: the Dockerfile, and the names,
// modes and contents of the files in the context directory that its
// .dockerignore does not exclude, with BuildKit's own pattern matching.
func contextHash(contextDir, dockerfile string) (string, error) {
	h := sha256.New() // its writes never fail
	_, _ = io.WriteString(h, "Dockerfile\x00")
	if err := hashFile(h, dockerfile); err != nil {
		return "", err
	}
	ignore, err := ignorePatterns(contextDir)
	if err != nil {
		return "", err
	}
	err = filepath.WalkDir(contextDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(contextDir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		excluded, err := ignore.MatchesOrParentMatches(rel)
		if err != nil {
			return err
		}
		if excluded {
			// With exceptions (!pattern), files below may be included again.
			if d.IsDir() && !ignore.Exclusions() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%v\x00", rel, info.Mode())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(h, "%s\x00", target)
		case info.Mode().IsRegular():
			return hashFile(h, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func ignorePatterns(contextDir string) (*patternmatcher.PatternMatcher, error) {
	var patterns []string
	f, err := os.Open(filepath.Join(contextDir, ".dockerignore"))
	switch {
	case err == nil:
		defer func() { _ = f.Close() }()
		if patterns, err = ignorefile.ReadAll(f); err != nil {
			return nil, fmt.Errorf("reading .dockerignore: %w", err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	return patternmatcher.New(patterns)
}

// hashFile adds the SHA-256 of a file's content to h.
func hashFile(h io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	content := sha256.New()
	if _, err := io.Copy(content, f); err != nil {
		return err
	}
	_, err = fmt.Fprintf(h, "%x\x00", content.Sum(nil))
	return err
}
