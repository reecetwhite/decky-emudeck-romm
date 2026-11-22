package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// ps2Save is a PS2 game's saves on PCSX2's folder memory card: each of the card's folders named for
// the game's serial, with every file in it, the card's index of the folder among them. On RomM
// they're a zip of the folders. The card's own files, and the other games' folders, are never part
// of it.
type ps2Save struct {
	card   string
	serial string
}

func (p ps2Save) mine(folder string) bool { return domain.PS2SaveOf(folder, p.serial) }

// owns is a file in one of the game's folders, or the folder itself as the game first makes it.
func (p ps2Save) owns(path string) bool {
	rel, err := filepath.Rel(p.card, path)
	if err != nil || !filepath.IsLocal(rel) {
		return false
	}
	folder, _, _ := strings.Cut(rel, string(filepath.Separator))
	return p.mine(folder)
}

// folders are the game's save folders on the card.
func (p ps2Save) folders() []string {
	entries, err := os.ReadDir(p.card)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && p.mine(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

func (p ps2Save) files() []string {
	var out []string
	for _, folder := range p.folders() {
		entries, err := os.ReadDir(filepath.Join(p.card, folder))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type().IsRegular() && !strings.HasSuffix(e.Name(), partSuffix) {
				out = append(out, filepath.Join(p.card, folder, e.Name()))
			}
		}
	}
	return out
}

func (p ps2Save) read() (localSave, error) {
	members := map[string][]byte{}
	var mod time.Time
	for _, path := range p.files() {
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
		rel, err := filepath.Rel(p.card, path)
		if err != nil {
			return localSave{}, err
		}
		members[filepath.ToSlash(rel)] = data
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

// write puts the save's folders on the card and removes the game's others, and the files of its
// folders the save lacks. Every other game's folders, and the card's own files, are left alone.
func (p ps2Save) write(data []byte) error {
	members, err := domain.ParseFolderUnit(data)
	if err != nil {
		return fmt.Errorf("%w, so PCSX2 can't use it", err)
	}
	for name := range members {
		folder, file, ok := strings.Cut(name, "/")
		if !ok || strings.Contains(file, "/") || !p.mine(folder) {
			return fmt.Errorf("RomM's save holds %s, which isn't in one of this game's save folders (%s), so PCSX2 can't use it", name, p.serial)
		}
	}
	old := p.files()
	for name, member := range members {
		if err := writeAtomic(filepath.Join(p.card, filepath.FromSlash(name)), member); err != nil {
			return err
		}
	}
	for _, path := range old {
		rel, err := filepath.Rel(p.card, path)
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
	// A folder the save no longer has is gone from the card.
	for _, folder := range p.folders() {
		_ = os.Remove(filepath.Join(p.card, folder)) // fails, and stays, unless it's empty
	}
	return nil
}

// ps2Target is where PCSX2 keeps the saves of the PS2 game at entry, on the folder memory card at
// card. why says why it can't be told.
func (s *Saves) ps2Target(entry, card string) (string, saveFormat, string) {
	if info, err := os.Stat(entry); err == nil && info.IsDir() {
		return "", nil, "The game is a folder, and only PS2 games kept as one disc image (.iso, .bin, .cue, .chd) sync their saves."
	}
	if domain.IsArchive(entry) {
		return "", nil, "The game is an archive, and PCSX2's saves sync only for disc images (.iso, .bin, .cue, .chd)."
	}
	serial, err := readCached(s, entry, func() (string, error) {
		cnf, err := s.ps2.SystemCNF(entry)
		if err != nil {
			return "", err
		}
		if serial := domain.PS2Serial(cnf); serial != "" {
			return serial, nil
		}
		return "", errors.New("its SYSTEM.CNF names no PS2 executable")
	})
	if err != nil {
		return "", nil, "The game's disc couldn't be read to tell its serial, which names its saves: " + err.Error()
	}
	return filepath.Join(card, domain.PS2SaveName(serial)), ps2Save{card: card, serial: serial}, ""
}

// ps2WatchDirs are the folders of PCSX2's folder memory card the save watch covers: the card, and
// each save folder on it.
func ps2WatchDirs(card string) []string {
	entries, err := os.ReadDir(card)
	if err != nil {
		return nil
	}
	out := []string{card}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(card, e.Name()))
		}
	}
	return out
}
