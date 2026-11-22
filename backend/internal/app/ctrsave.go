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

// ctrSave is a 3DS game's save as Azahar keeps it on its SD card: every file in the title's data
// folder, the save archive in data/00000001/ and its metadata beside it. On RomM it's a zip of the
// data folder.
type ctrSave struct {
	// title is the title's folder, which holds data.
	title string
}

func (c ctrSave) data() string { return filepath.Join(c.title, "data") }

// owns is any file in the title's folder, and the folder itself as Azahar first makes it: the save
// it then writes may land in folders the watch doesn't cover yet.
func (c ctrSave) owns(path string) bool {
	return path == c.title || strings.HasPrefix(path, c.title+string(filepath.Separator))
}

// files are the save's files in the data folder, by their full path.
func (c ctrSave) files() []string {
	var out []string
	_ = filepath.WalkDir(c.data(), func(path string, d fs.DirEntry, err error) error {
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

func (c ctrSave) read() (localSave, error) {
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
		rel, err := filepath.Rel(c.data(), path)
		if err != nil {
			return localSave{}, err
		}
		members[filepath.ToSlash(rel)] = data
		if info.ModTime().After(mod) {
			mod = info.ModTime().UTC()
		}
	}
	if !ctrArchive(members) {
		// Azahar makes the folder before the game formats its save; until then there's none.
		return localSave{}, nil
	}
	unit := domain.FolderUnit(members)
	return localSave{exists: true, data: unit, hash: domain.ContentHash(unit), mod: mod}, nil
}

// write replaces the data folder's files with the save's: the files it holds are written, and the
// folder's others removed, so the save archive is the one RomM keeps and nothing of the old one is
// left in it.
func (c ctrSave) write(data []byte) error {
	members, err := domain.ParseFolderUnit(data)
	if err != nil {
		return fmt.Errorf("%w, so Azahar can't use it", err)
	}
	if !ctrArchive(members) {
		return errors.New("RomM's save holds no 3DS save archive (00000001/), so Azahar can't use it")
	}
	old := c.files()
	for name, member := range members {
		if err := writeAtomic(filepath.Join(c.data(), filepath.FromSlash(name)), member); err != nil {
			return err
		}
	}
	for _, path := range old {
		rel, err := filepath.Rel(c.data(), path)
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
