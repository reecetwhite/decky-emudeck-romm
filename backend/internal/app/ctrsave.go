package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// saveOwner is a saveFormat that tells for itself which written files are its save's, for a save
// kept in a folder whose files aren't known until they're there.
type saveOwner interface {
	owns(path string) bool
}

// formatOwns says whether the file at path, just written, is part of the save f reads.
func formatOwns(f saveFormat, path string) bool {
	if o, ok := f.(saveOwner); ok {
		return o.owns(path)
	}
	return slices.Contains(f.files(), path)
}

// partSuffix marks a file writeAtomic hasn't finished writing.
const partSuffix = ".romm-part"

// titleSave is a save kept as a folder of files inside a title's folder, as Azahar keeps a 3DS
// game's and Dolphin a Wii game's: every file in the data folder. On RomM it's a zip of the data
// folder.
type titleSave struct {
	// title is the title's folder, which holds data. The emulator makes it, and other folders in it,
	// before the game first saves.
	title string
	// data is the folder the save is kept in.
	data string
	// emulator is the name of the emulator that keeps the save.
	emulator string
	// valid says whether a folder's files hold a save the game wrote, and missing what one that
	// doesn't lacks.
	valid   func(members map[string][]byte) bool
	missing string
}

// ctrSave is a 3DS game's save as Azahar keeps it on its SD card: the save archive in
// data/00000001/ and its metadata beside it.
func ctrSave(title string) titleSave {
	return titleSave{title: title, data: filepath.Join(title, "data"), emulator: "Azahar", valid: ctrArchive, missing: "no 3DS save archive (00000001/)"}
}

// owns is any file in the title's folder, and the folder itself as the emulator first makes it: the
// save it then writes may land in folders the watch doesn't cover yet.
func (c titleSave) owns(path string) bool {
	return path == c.title || strings.HasPrefix(path, c.title+string(filepath.Separator))
}

// files are the save's files in the data folder, by their full path.
func (c titleSave) files() []string {
	var out []string
	_ = filepath.WalkDir(c.data, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // a folder that isn't there holds no save
		}
		if d.Type().IsRegular() && !strings.HasSuffix(path, partSuffix) {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func (c titleSave) read() (localSave, error) {
	members := map[string][]byte{}
	var mod time.Time
	for _, path := range c.files() {
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
		rel, err := filepath.Rel(c.data, path)
		if err != nil {
			return localSave{}, err
		}
		members[filepath.ToSlash(rel)] = data
		if info.ModTime().After(mod) {
			mod = info.ModTime().UTC()
		}
	}
	if !c.valid(members) {
		// The emulator makes the folder before the game first saves; until then there's none.
		return localSave{}, nil
	}
	unit := domain.FolderUnit(members)
	return localSave{exists: true, data: unit, hash: domain.ContentHash(unit), mod: mod}, nil
}

// write replaces the data folder's files with the save's: the files it holds are written, and the
// folder's others removed, so the save is the one RomM keeps and nothing of the old one is left in
// it.
func (c titleSave) write(data []byte) error {
	members, err := domain.ParseFolderUnit(data)
	if err != nil {
		return fmt.Errorf("%w, so %s can't use it", err, c.emulator)
	}
	if !c.valid(members) {
		return fmt.Errorf("RomM's save holds %s, so %s can't use it", c.missing, c.emulator)
	}
	old := c.files()
	for name, member := range members {
		if err := writeAtomic(filepath.Join(c.data, filepath.FromSlash(name)), member); err != nil {
			return err
		}
	}
	for _, path := range old {
		rel, err := filepath.Rel(c.data, path)
		if err != nil {
			continue
		}
		if _, keep := members[filepath.ToSlash(rel)]; keep {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ctrArchive says whether a save folder's files hold a 3DS save archive: a file in 00000001/, the
// archive the game formats on its first save.
func ctrArchive(members map[string][]byte) bool {
	for name := range members {
		if strings.HasPrefix(name, "00000001/") {
			return true
		}
	}
	return false
}

// ctrTitleID reads the title ID of the 3DS game at path from its header.
func (s *Saves) ctrTitleID(path string) (uint64, error) {
	return readCached(s, path, func() (uint64, error) {
		f, err := os.Open(path)
		if err != nil {
			return 0, err
		}
		defer func() { _ = f.Close() }()
		header := make([]byte, domain.CTRHeaderSize)
		if _, err := f.ReadAt(header, 0); err != nil {
			return 0, fmt.Errorf("read its header: %w", err)
		}
		return domain.ParseCTRTitleID(header)
	})
}

// ctrTarget is where Azahar keeps the save of the 3DS game at entry, on its SD card at sdmc. why
// says why it can't be told.
func (s *Saves) ctrTarget(entry, sdmc string) (title string, titleID uint64, why string) {
	if info, err := os.Stat(entry); err == nil && info.IsDir() {
		return "", 0, "The game is a folder, and only 3DS games kept as one file (.3ds, .cci, .cxi) sync their saves."
	}
	if domain.IsArchive(entry) {
		return "", 0, "The game is an archive, and Azahar's saves sync only for games kept as a .3ds, .cci or .cxi file."
	}
	id, err := s.ctrTitleID(entry)
	if err != nil {
		return "", 0, "The game's header couldn't be read to tell its title ID, which names its save: " + err.Error()
	}
	return domain.CTRTitleDir(sdmc, id), id, ""
}

// ctrWatchDirs are the folders under Azahar's SD card at sdmc the save watch covers: each folder of
// its Nintendo 3DS tree down to the titles' save archives, which a watch of sdmc alone misses.
func ctrWatchDirs(sdmc string) []string {
	root := filepath.Join(sdmc, "Nintendo 3DS")
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // a folder that can't be read isn't watched
		}
		rel, _ := filepath.Rel(root, path)
		// Nintendo 3DS/<id0>/<id1>/title/<high>/<low>/data/00000001 is eight deep.
		if depth := strings.Count(rel, string(filepath.Separator)) + 1; rel != "." && depth > 8 {
			return filepath.SkipDir
		}
		if rel != "." {
			parts := strings.Split(rel, string(filepath.Separator))
			// Only the title tree: extdata and the rest hold no game saves the plugin syncs.
			if len(parts) >= 3 && parts[2] != "title" {
				return filepath.SkipDir
			}
		}
		out = append(out, path)
		return nil
	})
	return out
}
