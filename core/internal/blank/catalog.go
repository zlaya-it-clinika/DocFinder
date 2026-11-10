package blank

import (
	"embed"
	"path"
	"sync"

	"docfinder/core/internal/chunk"
)

//go:embed modules/*.docx
var moduleFiles embed.FS

var (
	catalogOnce sync.Once
	catalog     []Blank
	catalogErr  error
)

func Catalog() ([]Blank, error) {
	catalogOnce.Do(func() {
		entries, err := moduleFiles.ReadDir("modules")
		if err != nil {
			catalogErr = err
			return
		}
		loaded := make([]Blank, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || path.Ext(entry.Name()) != ".docx" {
				continue
			}
			body, readErr := moduleFiles.ReadFile("modules/" + entry.Name())
			if readErr != nil {
				catalogErr = readErr
				return
			}
			plain, textErr := chunk.Extract_DocumentText(entry.Name(), body)
			if textErr != nil {
				catalogErr = textErr
				return
			}
			parsed, parseErr := Parse_BlankText(entry.Name(), plain)
			if parseErr != nil {
				catalogErr = parseErr
				return
			}
			if name := read_DocxTitle(body); name != "" {
				parsed.Name = name
			}
			loaded = append(loaded, parsed)
		}
		catalog = loaded
	})
	return catalog, catalogErr
}
