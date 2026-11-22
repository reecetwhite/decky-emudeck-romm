package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

var cemu = domain.GameLauncher{
	Label: "Cemu (Standalone)", Command: "/usr/bin/bash /home/deck/Emulation/tools/launchers/cemu.sh -f -g %ROM%", Known: true}

// wiiuGame records Captain Toad, a game folder, as ROM 110 on platform 16, downloaded by the plugin
// and played in Cemu, and returns its save folder.
func (f *saveFixture) wiiuGame(t *testing.T) string {
	t.Helper()
	f.launchers["wiiu"] = cemu
	f.saves = f.newSaves()
	game := filepath.Join(f.root, "roms", "wiiu", "roms", "Captain Toad Treasure Tracker (USA)")
	writeFile(t, filepath.Join(game, "meta", "meta.xml"), []byte(`<menu><title_id type="hexBinary" length="8">0005000010180600</title_id></menu>`))
	writeFile(t, filepath.Join(game, "code", "app.xml"), []byte("<app/>"))
	f.catalog.mu.Lock()
	f.catalog.roms[110] = domain.ROM{ID: 110, PlatformID: 16, Name: "Captain Toad Treasure Tracker", FileName: filepath.Base(game)}
	f.catalog.mu.Unlock()
	files := []domain.PlacedFile{{Path: filepath.Join(game, "meta", "meta.xml")}, {Path: filepath.Join(game, "code", "app.xml")}}
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: 110, PlatformID: 16, Files: files}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.root, "saves", "Cemu", "saves", "00050000", "10180600")
}

func TestWiiUSaveRoundTrip(t *testing.T) {
	f := newSaveFixture(t)
	title := f.wiiuGame(t)
	if st := f.statusOf(t, 110); !st.Syncs || st.Path != filepath.Join(title, "0005000010180600.zip") || st.Emulator != "cemu" || st.Saver != "Cemu" {
		t.Fatalf("status %+v", st)
	}
	// Cemu writes the title's meta as the game starts; with nothing under user there's no save.
	writeFile(t, filepath.Join(title, "meta", "meta.xml"), []byte("meta"))
	if s := f.syncGame(t, 110); s.Uploaded != 0 {
		t.Fatalf("uploaded before the game saved: %+v", s)
	}
	writeFile(t, filepath.Join(title, "user", "80000001", "GameData.bin"), []byte("episode 1"))
	if s := f.syncGame(t, 110); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}
	head, _ := f.romm.head(110)
	m := zipMembers(t, head.data)
	if len(m) != 2 || string(m["user/80000001/GameData.bin"]) != "episode 1" || string(m["meta/meta.xml"]) != "meta" || head.FileName != "0005000010180600.zip" {
		t.Fatalf("RomM holds %s: %d members", head.FileName, len(m))
	}
	f.romm.otherDevice(110, "cemu", domain.FolderUnit(map[string][]byte{
		"user/80000001/GameData.bin": []byte("episode 3"), "meta/meta.xml": []byte("meta"),
	}))
	if s := f.syncGame(t, 110); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, filepath.Join(title, "user", "80000001", "GameData.bin")); got != "episode 3" {
		t.Fatalf("GameData.bin holds %q", got)
	}
	if s := f.syncGame(t, 110); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

func TestWiiUSaveRefusesOtherSaves(t *testing.T) {
	f := newSaveFixture(t)
	title := f.wiiuGame(t)
	f.romm.otherDevice(110, "cemu", domain.FolderUnit(map[string][]byte{"meta/meta.xml": []byte("no user")}))
	f.syncGame(t, 110)
	if _, err := os.Stat(title); err == nil {
		t.Fatal("a save with nothing under user was written")
	}
	if st := f.statusOf(t, 110); !strings.Contains(st.Problem, "Cemu can't use it") {
		t.Fatalf("status %+v", st)
	}
}

func TestWiiUWatchDirs(t *testing.T) {
	saves := t.TempDir()
	account := filepath.Join(saves, "00050000", "10180600", "user", "80000001")
	if err := os.MkdirAll(account, 0o755); err != nil {
		t.Fatal(err)
	}
	got := wiiuWatchDirs(saves)
	for _, want := range []string{filepath.Join(saves, "00050000"), filepath.Join(saves, "00050000", "10180600"), filepath.Dir(account), account} {
		if !slices.Contains(got, want) {
			t.Errorf("%s isn't watched: %v", want, got)
		}
	}
}
