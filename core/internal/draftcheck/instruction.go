package draftcheck

import (
	"embed"
	"sync"

	"docfinder/core/internal/chunk"
)

//go:embed instruction.pdf
var instructionFile embed.FS

var (
	instructionOnce   sync.Once
	instructionChunks []chunk.TextChunk
	instructionPlain  string
	instructionErr    error
)

func instruction_Text() (string, []chunk.TextChunk, error) {
	instructionOnce.Do(func() {
		body, err := instructionFile.ReadFile("instruction.pdf")
		if err != nil {
			instructionErr = err
			return
		}
		plain, err := chunk.Extract_DocumentText("instruction.pdf", body)
		if err != nil {
			instructionErr = err
			return
		}
		instructionPlain = plain
		instructionChunks = chunk.Split_DocumentText(plain)
	})
	return instructionPlain, instructionChunks, instructionErr
}
