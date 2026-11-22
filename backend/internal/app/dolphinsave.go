package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// wiiSave is a Wii game's save as Dolphin keeps it in the NAND: the files in the title's data
// folder, among them the banner every Wii save has.
func wiiSave(title string) titleSave {
	return titleSave{title: title, data: filepath.Join(title, "data"), emulator: "Dolphin", valid: wiiBanner, missing: "no Wii save banner (banner.bin)"}
}

// wiiBanner says whether a save folder's files hold a Wii save: every one has its banner.
func wiiBanner(members map[string][]byte) bool {
	_, ok := members["banner.bin"]
	return ok
}

// dolphinDisc reads the game on the GameCube or Wii disc image at path from its header.
func (s *Saves) dolphinDisc(path string) (domain.DolphinDisc, error) {
	return readCached(s, path, func() (domain.DolphinDisc, error) {
		f, err := os.Open(path)
		if err != nil {
			return domain.DolphinDisc{}, err
		}
		defer func() { _ = f.Close() }()
		header := make([]byte, domain.DolphinHeaderSize)
		n, err := f.ReadAt(header, 0)
		if err != nil && !errors.Is(err, io.EOF) {
			return domain.DolphinDisc{}, fmt.Errorf("read its header: %w", err)
		}
		return domain.ParseDolphinDisc(header[:n])
	})
}

// dolphinTarget is where Dolphin keeps the save of the game at entry in system's folder, nand being
// its Wii NAND. why says why it can't be told, or why the game's saves don't sync.
func (s *Saves) dolphinTarget(entry, system, nand string) (path string, format saveFormat, why string) {
	if system != "wii" {
		return "", nil, "GameCube saves don't sync yet."
	}
	if info, err := os.Stat(entry); err == nil && info.IsDir() {
		return "", nil, "The game is a folder, and only Wii games kept as one disc image sync their saves."
	}
	if domain.IsArchive(entry) {
		return "", nil, "The game is an archive, and Dolphin's saves sync only for disc images (.rvz, .iso, .wbfs, .wia, .ciso)."
	}
	disc, err := s.dolphinDisc(entry)
	if err != nil {
		return "", nil, "The game's disc header couldn't be read to tell its game ID, which names its save: " + err.Error()
	}
	if !disc.Wii {
		return "", nil, "The game in the Wii folder is a GameCube disc, and GameCube saves don't sync yet."
	}
	title := domain.WiiTitleDir(nand, disc)
	return filepath.Join(title, domain.WiiSaveName(disc)), wiiSave(title), ""
}

// wiiWatchDirs are the folders of Dolphin's NAND at nand the save watch covers: the titles of games
// played from disc, each title's folder and its data folder, which a watch of nand alone misses.
func wiiWatchDirs(nand string) []string {
	root := domain.WiiTitlesDir(nand)
	var out []string
	// Before any game saves, the titles' folder isn't there; Dolphin makes it under title.
	if info, err := os.Stat(filepath.Join(nand, "title")); err == nil && info.IsDir() {
		out = append(out, filepath.Join(nand, "title"))
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // a folder that can't be read isn't watched
		}
		rel, _ := filepath.Rel(root, path)
		// title/00010000/<title>/data is two below the titles; deeper folders hold no save files
		// Dolphin writes as the game saves.
		if rel != "." && strings.Count(rel, string(filepath.Separator)) >= 2 {
			return filepath.SkipDir
		}
		if rel != "." && strings.Count(rel, string(filepath.Separator)) == 1 && d.Name() != "data" {
			return filepath.SkipDir
		}
		out = append(out, path)
		return nil
	})
	return out
}
