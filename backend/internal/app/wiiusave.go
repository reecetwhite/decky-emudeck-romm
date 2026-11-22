package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// wiiuSave is a Wii U game's save as Cemu keeps it in the MLC: the title's whole save folder, its
// user folders and its meta.
func wiiuSave(title string) titleSave {
	return titleSave{title: title, data: title, emulator: "Cemu", valid: wiiuUser, missing: "no Wii U save (user/)"}
}

// wiiuUser says whether a save folder's files hold a Wii U save: one the game wrote under user.
func wiiuUser(members map[string][]byte) bool {
	for name := range members {
		if strings.HasPrefix(name, "user/") {
			return true
		}
	}
	return false
}

// wiiuTitleID reads the title ID of the Wii U game at entry: a .wua's game title, or the one a
// game folder's meta.xml or app.xml names.
func (s *Saves) wiiuTitleID(entry string) (uint64, error) {
	info, err := os.Stat(entry)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		for _, rel := range []string{"meta/meta.xml", "code/app.xml"} {
			if data, err := os.ReadFile(filepath.Join(entry, rel)); err == nil {
				return domain.WiiUMetaTitleID(data)
			}
		}
		return 0, errors.New("the game's folder has no meta/meta.xml or code/app.xml")
	}
	if !strings.EqualFold(filepath.Ext(entry), ".wua") {
		return 0, errors.New("only .wua files and game folders are read; " + filepath.Ext(entry) + " isn't")
	}
	return readCached(s, entry, func() (uint64, error) {
		f, err := os.Open(entry)
		if err != nil {
			return 0, err
		}
		defer func() { _ = f.Close() }()
		return domain.WUATitleID(f, info.Size())
	})
}

// wiiuTarget is where Cemu keeps the save of the Wii U game at entry, under the MLC's usr/save at
// saves. why says why it can't be told.
func (s *Saves) wiiuTarget(entry, saves string) (string, saveFormat, string) {
	id, err := s.wiiuTitleID(entry)
	if err != nil {
		return "", nil, "The game couldn't be read to tell its title ID, which names its save: " + err.Error()
	}
	title := domain.WiiUTitleDir(saves, id)
	return filepath.Join(title, domain.WiiUSaveName(id)), wiiuSave(title), ""
}

// wiiuWatchDirs are the folders under the MLC's usr/save at saves the save watch covers: the games'
// folders down to each account's, which a watch of saves alone misses.
func wiiuWatchDirs(saves string) []string {
	root := domain.WiiUGamesDir(saves)
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // a folder that can't be read isn't watched
		}
		rel, _ := filepath.Rel(root, path)
		// <title>/user/<account> is three below the games' folder.
		if rel != "." && strings.Count(rel, string(filepath.Separator)) >= 3 {
			return filepath.SkipDir
		}
		out = append(out, path)
		return nil
	})
	return out
}
