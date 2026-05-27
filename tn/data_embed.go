package tn

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

//go:embed "chinese/data"
var tnChineseData embed.FS
//go:embed "english/data"
var tnEnglishData embed.FS
//go:embed "japanese/data"
var tnJapaneseData embed.FS

type embedSource struct {
	name    string   // destination dir name, e.g. "chinese", "itn/chinese"
	pattern string   // embed FS root pattern, e.g. "chinese", "english"
	fs      fs.FS
}

var (
	embedSources []embedSource
	extractOnce  sync.Once
	extractRoot  string
)

// RegisterEmbedFS registers an additional embedded filesystem for data files.
// Called from itn package's init() to register ITN data.
// name: destination directory (e.g. "itn/chinese")
// pattern: embed pattern root (e.g. "chinese") - must match //go:embed prefix
// filesys: the embed.FS
func RegisterEmbedFS(name, pattern string, filesys fs.FS) {
	embedSources = append(embedSources, embedSource{name, pattern, filesys})
}

func init() {
	RegisterEmbedFS("chinese", "chinese", tnChineseData)
	RegisterEmbedFS("english", "english", tnEnglishData)
	RegisterEmbedFS("japanese", "japanese", tnJapaneseData)
}

func ensureExtracted() string {
	extractOnce.Do(func() {
		d, err := os.MkdirTemp("", "wetext_embed_*")
		if err != nil {
			panic("tn: failed to create embed extract dir: " + err.Error())
		}
		extractRoot = d
		for _, src := range embedSources {
			// Extract embed pattern (e.g. "chinese/") to destination (e.g. "<dir>/chinese/")
			extractFS(src.fs, src.pattern, filepath.Join(d, src.name))
		}
	})
	return extractRoot
}

func extractFS(fsys fs.FS, srcPath, dstPath string) {
	entries, err := fs.ReadDir(fsys, srcPath)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dstPath, 0755); err != nil {
		panic("tn: failed to create dir " + dstPath + ": " + err.Error())
	}
	for _, entry := range entries {
		src := filepath.ToSlash(filepath.Join(srcPath, entry.Name()))
		dst := filepath.Join(dstPath, entry.Name())
		if entry.IsDir() {
			extractFS(fsys, src, dst)
		} else {
			data, err := fs.ReadFile(fsys, src)
			if err != nil {
				panic("tn: failed to read " + src + ": " + err.Error())
			}
			if err := os.WriteFile(dst, data, 0644); err != nil {
				panic("tn: failed to write " + dst + ": " + err.Error())
			}
		}
	}
}

// ExtractDir returns the directory where embedded data files are extracted.
// Used by GetAbsPath in production mode.
func ExtractDir() string {
	return ensureExtracted()
}