package app

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// azahar is how EmuDeck's custom_systems have ES-DE launch 3DS games: through its launcher script.
var azahar = domain.GameLauncher{
	Label: "Azahar (Standalone)", Command: "/usr/bin/bash /home/deck/Emulation/tools/launchers/azahar.sh %ROM%", Known: true}

// mario3DLand is Super Mario 3D Land's title ID.
const mario3DLand = 0x0004000000054000

// ctrGame records Super Mario 3D Land, ROM id 70 on platform 8, as downloaded by the plugin and
// played in Azahar, and returns its save's data folder.
func (f *saveFixture) ctrGame(t *testing.T) string {
	t.Helper()
	f.launchers["n3ds"] = azahar
	f.saves = f.newSaves()
	header := make([]byte, 0x4000)
	copy(header[0x100:], "NCSD")
	binary.LittleEndian.PutUint64(header[0x108:], mario3DLand)
	path := filepath.Join(f.root, "roms", "n3ds", "Super Mario 3D Land (USA).3ds")
	writeFile(t, path, header)
	f.catalog.mu.Lock()
	f.catalog.roms[70] = domain.ROM{ID: 70, PlatformID: 8, Name: "Super Mario 3D Land", FileName: filepath.Base(path)}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: 70, PlatformID: 8, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(domain.CTRTitleDir(filepath.Join(f.root, "saves", "azahar", "saves"), mario3DLand), "data")
}

// ctrSaveFiles writes a save as Azahar does: the archive's files, and its metadata beside it.
func ctrSaveFiles(t *testing.T, data, game string) {
	t.Helper()
	writeFile(t, filepath.Join(data, "00000001", "GameData.bin"), []byte(game))
	writeFile(t, filepath.Join(data, "00000001.metadata"), []byte("meta"))
}

func TestCTRSaveUploadsAsAZipOfItsFolder(t *testing.T) {
	f := newSaveFixture(t)
	data := f.ctrGame(t)
	title := filepath.Dir(data)
	if st := f.statusOf(t, 70); !st.Syncs || st.Path != filepath.Join(title, "0004000000054000.zip") || st.Emulator != "azahar" || st.Saver != "Azahar" {
		t.Fatalf("status %+v", st)
	}
	// Azahar makes the folder before the game formats its save: nothing to upload yet.
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if s := f.syncGame(t, 70); s.Uploaded != 0 {
		t.Fatalf("empty folder uploaded: %+v", s)
	}

	ctrSaveFiles(t, data, "world 1-1")
	if s := f.syncGame(t, 70); s.Uploaded != 1 {
		t.Fatalf("summary %+v", s)
	}
	head, _ := f.romm.head(70)
	members := zipMembers(t, head.data)
	if len(members) != 2 || string(members["00000001/GameData.bin"]) != "world 1-1" || string(members["00000001.metadata"]) != "meta" ||
		head.FileName != "0004000000054000.zip" || head.Emulator != "azahar" {
		t.Fatalf("RomM holds %s from %s: %v", head.FileName, head.Emulator, slices.Collect(func(yield func(string) bool) {
			for k := range members {
				if !yield(k) {
					return
				}
			}
		}))
	}
	if s := f.syncGame(t, 70); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

// A save from RomM replaces the folder's files, leaving none of the old save's behind, and the old
// save is backed up first.
func TestCTRSaveDownloadsIntoItsFolder(t *testing.T) {
	f := newSaveFixture(t)
	data := f.ctrGame(t)
	ctrSaveFiles(t, data, "old")
	writeFile(t, filepath.Join(data, "00000001", "Stale.bin"), []byte("stale"))
	if s := f.syncGame(t, 70); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}

	f.romm.otherDevice(70, "azahar", domain.FolderUnit(map[string][]byte{
		"00000001/GameData.bin": []byte("world 3-2"), "00000001.metadata": []byte("meta2"),
	}))
	if s := f.syncGame(t, 70); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, filepath.Join(data, "00000001", "GameData.bin")); got != "world 3-2" {
		t.Fatalf("GameData.bin holds %q", got)
	}
	if got := readFile(t, filepath.Join(data, "00000001.metadata")); got != "meta2" {
		t.Fatalf("metadata holds %q", got)
	}
	gone(t, filepath.Join(data, "00000001", "Stale.bin"))
	backups, err := os.ReadDir(f.saves.backupDir(70))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups %v, %v", backups, err)
	}
	if s := f.syncGame(t, 70); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

// A save from RomM that isn't a 3DS save folder never replaces the Deck's.
func TestCTRSaveRefusesOtherSaves(t *testing.T) {
	f := newSaveFixture(t)
	data := f.ctrGame(t)
	f.romm.otherDevice(70, "citra", []byte("a raw .sav"))
	f.syncGame(t, 70)
	if _, err := os.Stat(data); err == nil {
		t.Fatal("a save that isn't a 3DS save folder was written")
	}
	if st := f.statusOf(t, 70); !strings.Contains(st.Problem, "Azahar can't use it") {
		t.Fatalf("status %+v", st)
	}
}

// The watch reports files deep in the title's folder, and the folder itself as Azahar first makes
// it; both are the game's.
func TestCTRSaveOwnsItsFolder(t *testing.T) {
	f := newSaveFixture(t)
	data := f.ctrGame(t)
	title := filepath.Dir(data)
	c := ctrSave{title: title}
	for _, path := range []string{title, filepath.Join(data, "00000001", "GameData.bin"), filepath.Join(data, "00000001.metadata")} {
		if !formatOwns(c, path) {
			t.Errorf("%s isn't the game's", path)
		}
	}
	if formatOwns(c, title+"0") || formatOwns(c, filepath.Dir(title)) {
		t.Error("a neighbouring title's folder is the game's")
	}
}

// The watch covers the title tree down to the save archives, and nothing else of the SD card.
func TestCTRWatchDirs(t *testing.T) {
	sdmc := t.TempDir()
	data := filepath.Join(domain.CTRTitleDir(sdmc, mario3DLand), "data")
	ctrSaveFiles(t, data, "save")
	extdata := filepath.Join(sdmc, "Nintendo 3DS", "00000000000000000000000000000000", "00000000000000000000000000000000", "extdata", "00000000", "0000abcd")
	if err := os.MkdirAll(extdata, 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := ctrWatchDirs(sdmc)
	for _, want := range []string{data, filepath.Join(data, "00000001"), filepath.Dir(filepath.Dir(data))} {
		if !slices.Contains(dirs, want) {
			t.Errorf("%s isn't watched: %v", want, dirs)
		}
	}
	if slices.Contains(dirs, extdata) {
		t.Errorf("extdata is watched")
	}
}
