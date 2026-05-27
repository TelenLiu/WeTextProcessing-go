package itn

import (
	"embed"
	"io/fs"

	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

//go:embed "chinese/data"
var itnChineseData embed.FS

//go:embed "japanese/data"
var itnJapaneseData embed.FS

func init() {
	tn.RegisterEmbedFS("itn/chinese", "chinese", fs.FS(itnChineseData))
	tn.RegisterEmbedFS("itn/japanese", "japanese", fs.FS(itnJapaneseData))
}