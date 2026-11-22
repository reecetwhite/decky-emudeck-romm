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

// dolphin is how ES-DE launches Wii games with Dolphin, as a gamelist's alternative emulator names
// it when ES-DE's own systems can't be read.
var dolphin = domain.GameLauncher{Label: "Dolphin (Standalone)"}

// wiiDisc is the start of an .rvz of the Wii game with id.
func wiiDisc(id string) []byte {
	b := make([]byte, 0x9000)
	copy(b, "RVZ\x01")
	copy(b[0x58:], id)
	binary.BigEndian.PutUint32(b[0x58+0x18:], 0x5D1C9EA3)
	return b
}

// wiiGame records Super Mario Galaxy, ROM id 80 on platform 13, as downloaded by the plugin and
// played in Dolphin, and returns its save's data folder.
func (f *saveFixture) wiiGame(t *testing.T, image []byte) string {
	t.Helper()
	f.launchers["wii"] = dolphin
	f.saves = f.newSaves()
	path := filepath.Join(f.root, "roms", "wii", "Super Mario Galaxy (USA).rvz")
	writeFile(t, path, image)
	f.catalog.mu.Lock()
	f.catalog.roms[80] = domain.ROM{ID: 80, PlatformID: 13, Name: "Super Mario Galaxy", FileName: filepath.Base(path)}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: 80, PlatformID: 13, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.root, "saves", "dolphin", "Wii", "title", "00010000", "524d4745", "data")
}

func TestWiiSaveRoundTrip(t *testing.T) {
	f := newSaveFixture(t)
	data := f.wiiGame(t, wiiDisc("RMGE01"))
	if st := f.statusOf(t, 80); !st.Syncs || st.Path != filepath.Join(filepath.Dir(data), "RMGE01.zip") || st.Emulator != "dolphin" || st.Saver != "Dolphin" {
		t.Fatalf("status %+v", st)
	}
	// Dolphin writes the title's content when the game starts; with no banner there's no save yet.
	writeFile(t, filepath.Join(filepath.Dir(data), "content", "title.tmd"), []byte("tmd"))
	if s := f.syncGame(t, 80); s.Uploaded != 0 {
		t.Fatalf("uploaded before the game saved: %+v", s)
	}

	writeFile(t, filepath.Join(data, "GameData.bin"), []byte("star 1"))
	writeFile(t, filepath.Join(data, "banner.bin"), []byte("banner"))
	if s := f.syncGame(t, 80); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}
	head, _ := f.romm.head(80)
	if m := zipMembers(t, head.data); len(m) != 2 || string(m["GameData.bin"]) != "star 1" || head.FileName != "RMGE01.zip" || head.Emulator != "dolphin" {
		t.Fatalf("RomM holds %s: %d members", head.FileName, len(m))
	}

	f.romm.otherDevice(80, "dolphin", domain.FolderUnit(map[string][]byte{"GameData.bin": []byte("star 42"), "banner.bin": []byte("banner")}))
	if s := f.syncGame(t, 80); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, filepath.Join(data, "GameData.bin")); got != "star 42" {
		t.Fatalf("GameData.bin holds %q", got)
	}
	// The title's content isn't part of the save, so a download leaves it alone.
	if got := readFile(t, filepath.Join(filepath.Dir(data), "content", "title.tmd")); got != "tmd" {
		t.Fatalf("title.tmd holds %q", got)
	}
	if s := f.syncGame(t, 80); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

// A save that isn't a Wii save folder never replaces the Deck's.
func TestWiiSaveRefusesOtherSaves(t *testing.T) {
	f := newSaveFixture(t)
	data := f.wiiGame(t, wiiDisc("RMGE01"))
	f.romm.otherDevice(80, "dolphin", domain.FolderUnit(map[string][]byte{"GameData.bin": []byte("no banner")}))
	f.syncGame(t, 80)
	if _, err := os.Stat(data); err == nil {
		t.Fatal("a save with no banner was written")
	}
	if st := f.statusOf(t, 80); !strings.Contains(st.Problem, "Dolphin can't use it") {
		t.Fatalf("status %+v", st)
	}
}

// A GameCube disc in the Wii folder doesn't sync yet, and says so.
func TestWiiFolderGameCubeDisc(t *testing.T) {
	f := newSaveFixture(t)
	gc := make([]byte, 0x9000)
	copy(gc, "RVZ\x01")
	copy(gc[0x58:], "GMSE01")
	binary.BigEndian.PutUint32(gc[0x58+0x1C:], 0xC2339F3D)
	f.wiiGame(t, gc)
	if st := f.statusOf(t, 80); st.Syncs || !strings.Contains(st.Why, "GameCube") {
		t.Fatalf("status %+v", st)
	}
}

func TestWiiWatchDirs(t *testing.T) {
	nand := t.TempDir()
	if got := wiiWatchDirs(nand); len(got) != 0 {
		t.Fatalf("watching folders that aren't there: %v", got)
	}
	title := filepath.Join(nand, "title", "00010000", "524d4745")
	for _, d := range []string{"data", "content"} {
		if err := os.MkdirAll(filepath.Join(title, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := wiiWatchDirs(nand)
	for _, want := range []string{filepath.Join(nand, "title"), filepath.Dir(title), title, filepath.Join(title, "data")} {
		if !slices.Contains(got, want) {
			t.Errorf("%s isn't watched: %v", want, got)
		}
	}
	if slices.Contains(got, filepath.Join(title, "content")) {
		t.Error("content is watched")
	}
}
