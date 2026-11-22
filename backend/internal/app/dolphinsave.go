package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// dolphinTarget is where Dolphin keeps the save of the game at entry in system's folder: a Wii
// game's in its NAND at nand, a GameCube game's in its GCI folder for slot A. why says why it can't
// be told, or why the game's saves don't sync.
func (s *Saves) dolphinTarget(state domain.InstallState, entry, system, nand string) (path string, format saveFormat, why string) {
	if info, err := os.Stat(entry); err == nil && info.IsDir() {
		return "", nil, "The game is a folder, and only games kept as disc images or a playlist of them sync their saves."
	}
	if domain.IsArchive(entry) {
		return "", nil, "The game is an archive, and Dolphin's saves sync only for disc images (.rvz, .iso, .wbfs, .wia, .ciso)."
	}
	if strings.EqualFold(filepath.Ext(entry), ".m3u") {
		first, err := firstPlaylistEntry(entry)
		if err != nil {
			return "", nil, "The game's playlist couldn't be read to find its first disc: " + err.Error()
		}
		entry = first
	}
	disc, err := s.dolphinDisc(entry)
	if err != nil {
		return "", nil, "The game's disc header couldn't be read to tell its game ID, which names its save: " + err.Error()
	}
	switch {
	case system == "wii" && disc.Wii:
		title := domain.WiiTitleDir(nand, disc)
		return filepath.Join(title, domain.WiiSaveName(disc)), wiiSave(title), ""
	case system == "gc" && !disc.Wii:
		card, why := s.gcCard(state, disc)
		if why != "" {
			return "", nil, why
		}
		return filepath.Join(card, domain.GCSaveName(disc)), gciSave{dir: card, prefix: domain.GCIPrefix(disc)}, ""
	case disc.Wii:
		return "", nil, "The game is a Wii disc in the GameCube folder; move it to the Wii folder for its saves to sync."
	}
	return "", nil, "The game is a GameCube disc in the Wii folder; move it to the GameCube folder for its saves to sync."
}

// gcRoot is the folder Dolphin keeps GameCube memory cards in, links followed, and whether it's the
// card in slot A itself, as Dolphin's settings name it, rather than the folder of region folders.
// why says why GameCube saves don't sync.
func (s *Saves) gcRoot(state domain.InstallState) (dir string, card bool, why string) {
	cfg := s.configs.SaveConfig("dolphin-gc")
	switch {
	case cfg.Problem != "":
		return "", false, cfg.Problem
	case cfg.Dir != "":
		return cfg.Dir, true, ""
	}
	if state.EmuDeck.SavesPath == "" {
		return "", false, "Dolphin's GameCube saves folder wasn't found. Set it up in EmuDeck, then check Setup status again."
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(state.EmuDeck.SavesPath, "dolphin", "GC"))
	if err != nil {
		return "", false, "Dolphin's GameCube saves folder wasn't found. Set it up in EmuDeck, then check Setup status again."
	}
	return dir, false, ""
}

// gcCard is the GCI folder of slot A that Dolphin keeps the GameCube game disc's saves in.
func (s *Saves) gcCard(state domain.InstallState, disc domain.DolphinDisc) (string, string) {
	root, card, why := s.gcRoot(state)
	if why != "" || card {
		return root, why
	}
	return filepath.Join(root, domain.GCRegionDir(disc), "Card A"), ""
}

// gcWatchDirs are the folders under Dolphin's GameCube folder the save watch covers: each region's
// and its slot A card's, as they're there.
func (s *Saves) gcWatchDirs(state domain.InstallState) []string {
	root, card, why := s.gcRoot(state)
	if why != "" {
		return nil
	}
	if card {
		return existingDirs(root)
	}
	var out []string
	for _, region := range []string{"USA", "EUR", "JAP"} {
		out = append(out, existingDirs(filepath.Join(root, region), filepath.Join(root, region, "Card A"))...)
	}
	return append(existingDirs(root), out...)
}

func existingDirs(dirs ...string) []string {
	var out []string
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// firstPlaylistEntry is the first disc an .m3u playlist lists, relative to the playlist's folder
// unless it's a full path.
func firstPlaylistEntry(m3u string) (string, error) {
	data, err := os.ReadFile(m3u)
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if filepath.IsAbs(line) {
			return line, nil
		}
		return filepath.Join(filepath.Dir(m3u), filepath.FromSlash(line)), nil
	}
	return "", errors.New("it lists no disc")
}

// gciSave is a GameCube game's saves in a GCI folder: a .gci file per save, each named for the
// game's maker and title codes. On RomM they're a zip of the files by name.
type gciSave struct {
	dir    string
	prefix string
}

func (g gciSave) mine(name string) bool {
	return strings.HasPrefix(name, g.prefix) && strings.EqualFold(filepath.Ext(name), ".gci") && !strings.ContainsAny(name, `/\`)
}

func (g gciSave) owns(path string) bool {
	return filepath.Dir(path) == g.dir && g.mine(filepath.Base(path))
}

func (g gciSave) files() []string {
	entries, err := os.ReadDir(g.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && g.mine(e.Name()) {
			out = append(out, filepath.Join(g.dir, e.Name()))
		}
	}
	return out
}

func (g gciSave) read() (localSave, error) {
	members := map[string][]byte{}
	var mod time.Time
	for _, path := range g.files() {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return localSave{}, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return localSave{}, err
		}
		members[filepath.Base(path)] = data
		if info.ModTime().After(mod) {
			mod = info.ModTime().UTC()
		}
	}
	if len(members) == 0 {
		return localSave{}, nil
	}
	unit := domain.FolderUnit(members)
	return localSave{exists: true, data: unit, hash: domain.ContentHash(unit), mod: mod}, nil
}

// write puts the save's .gci files in the folder and removes the game's others, leaving every other
// game's saves in the folder alone.
func (g gciSave) write(data []byte) error {
	members, err := domain.ParseFolderUnit(data)
	if err != nil {
		return fmt.Errorf("%w, so Dolphin can't use it", err)
	}
	for name := range members {
		if !g.mine(name) {
			return fmt.Errorf("RomM's save holds %s, which isn't one of this game's GameCube saves (%s*.gci), so Dolphin can't use it", name, g.prefix)
		}
	}
	old := g.files()
	for name, member := range members {
		if err := writeAtomic(filepath.Join(g.dir, name), member); err != nil {
			return err
		}
	}
	for _, path := range old {
		if _, keep := members[filepath.Base(path)]; keep {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
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
