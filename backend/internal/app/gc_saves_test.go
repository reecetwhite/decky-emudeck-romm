package app

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// gcDisc is the start of an .rvz of the GameCube game with id, disc number disc.
func gcDisc(id string, disc byte) []byte {
	b := make([]byte, 0x9000)
	copy(b, "RVZ\x01")
	copy(b[0x58:], id)
	b[0x58+6] = disc
	binary.BigEndian.PutUint32(b[0x58+0x1C:], 0xC2339F3D)
	return b
}

// gcGame records the GameCube game in file as ROM id on platform 14, downloaded by the plugin and
// played in Dolphin, and returns slot A's GCI folder for USA games.
func (f *saveFixture) gcGame(t *testing.T, id int, file string, image []byte) string {
	t.Helper()
	f.launchers["gc"] = dolphin
	f.saves = f.newSaves()
	// EmuDeck links saves/dolphin/GC to Dolphin's GameCube folder.
	if err := os.MkdirAll(filepath.Join(f.root, "saves", "dolphin", "GC"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "roms", "gc", file)
	writeFile(t, path, image)
	f.catalog.mu.Lock()
	f.catalog.roms[id] = domain.ROM{ID: id, PlatformID: 14, Name: strings.TrimSuffix(file, filepath.Ext(file)), FileName: file}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: id, PlatformID: 14, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.root, "saves", "dolphin", "GC", "USA", "Card A")
}

// Only the game's own .gci files sync; the other games' in the same folder are left alone, both ways.
func TestGCSavesRoundTrip(t *testing.T) {
	f := newSaveFixture(t)
	card := f.gcGame(t, 90, "Super Mario Sunshine (USA).rvz", gcDisc("GMSE01", 0))
	if st := f.statusOf(t, 90); !st.Syncs || st.Path != filepath.Join(card, "GMSE01.zip") || st.Emulator != "dolphin" {
		t.Fatalf("status %+v", st)
	}
	writeFile(t, filepath.Join(card, "01-GMSE-super_mario_sunshine.gci"), []byte("shine 1"))
	writeFile(t, filepath.Join(card, "01-GM4E-mario_kart.gci"), []byte("kart"))
	if s := f.syncGame(t, 90); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}
	head, _ := f.romm.head(90)
	if m := zipMembers(t, head.data); len(m) != 1 || string(m["01-GMSE-super_mario_sunshine.gci"]) != "shine 1" || head.FileName != "GMSE01.zip" {
		t.Fatalf("RomM holds %s: %v members", head.FileName, len(m))
	}

	writeFile(t, filepath.Join(card, "01-GMSE-stale.gci"), []byte("stale"))
	f.romm.otherDevice(90, "dolphin", domain.FolderUnit(map[string][]byte{"01-GMSE-super_mario_sunshine.gci": []byte("shine 120")}))
	if s := f.syncGame(t, 90); s.Conflicts != 1 {
		// The stale file changed the Deck's save since it last synced: that's a conflict, not a
		// download.
		t.Fatalf("expected a conflict, got %+v", s)
	}
	if err := os.Remove(filepath.Join(card, "01-GMSE-stale.gci")); err != nil {
		t.Fatal(err)
	}
	if s := f.syncGame(t, 90); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, filepath.Join(card, "01-GMSE-super_mario_sunshine.gci")); got != "shine 120" {
		t.Fatalf("save holds %q", got)
	}
	if got := readFile(t, filepath.Join(card, "01-GM4E-mario_kart.gci")); got != "kart" {
		t.Fatalf("another game's save changed: %q", got)
	}
}

// A download removes the game's saves RomM's lacks, and never another game's.
func TestGCSaveDownloadReplacesOnlyTheGames(t *testing.T) {
	f := newSaveFixture(t)
	card := f.gcGame(t, 90, "Super Mario Sunshine (USA).rvz", gcDisc("GMSE01", 0))
	writeFile(t, filepath.Join(card, "01-GM4E-mario_kart.gci"), []byte("kart"))
	f.romm.otherDevice(90, "dolphin", domain.FolderUnit(map[string][]byte{"01-GMSE-super_mario_sunshine.gci": []byte("shine 5")}))
	if s := f.syncGame(t, 90); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, filepath.Join(card, "01-GMSE-super_mario_sunshine.gci")); got != "shine 5" {
		t.Fatalf("save holds %q", got)
	}
	if got := readFile(t, filepath.Join(card, "01-GM4E-mario_kart.gci")); got != "kart" {
		t.Fatalf("another game's save changed: %q", got)
	}
}

// A save from RomM holding another game's .gci never lands in the folder.
func TestGCSaveRefusesOtherGamesFiles(t *testing.T) {
	f := newSaveFixture(t)
	card := f.gcGame(t, 90, "Super Mario Sunshine (USA).rvz", gcDisc("GMSE01", 0))
	f.romm.otherDevice(90, "dolphin", domain.FolderUnit(map[string][]byte{"01-GM4E-mario_kart.gci": []byte("not ours")}))
	f.syncGame(t, 90)
	gone(t, filepath.Join(card, "01-GM4E-mario_kart.gci"))
	if st := f.statusOf(t, 90); !strings.Contains(st.Problem, "Dolphin can't use it") {
		t.Fatalf("status %+v", st)
	}
}

// The discs of a game on several share its game ID, so the first one syncs the save for all.
func TestGCDiscsShareTheirSave(t *testing.T) {
	f := newSaveFixture(t)
	f.gcGame(t, 91, "Killer7 (USA) (Disc 1).rvz", gcDisc("GK7E08", 0))
	f.gcGame(t, 92, "Killer7 (USA) (Disc 2).rvz", gcDisc("GK7E08", 1))
	if st := f.statusOf(t, 91); !st.Syncs {
		t.Fatalf("disc 1: %+v", st)
	}
	if st := f.statusOf(t, 92); st.Syncs || !strings.Contains(st.Why, "Killer7 (USA) (Disc 1)") {
		t.Fatalf("disc 2: %+v", st)
	}
}

// A game kept as a playlist of its discs is read by its first.
func TestGCPlaylist(t *testing.T) {
	f := newSaveFixture(t)
	writeFile(t, filepath.Join(f.root, "roms", "gc", "Killer7", "Killer7 (Disc 1).rvz"), gcDisc("GK7E08", 0))
	card := f.gcGame(t, 91, "Killer7.m3u", []byte("# discs\nKiller7/Killer7 (Disc 1).rvz\nKiller7/Killer7 (Disc 2).rvz\n"))
	if st := f.statusOf(t, 91); !st.Syncs || st.Path != filepath.Join(card, "GK7E08.zip") {
		t.Fatalf("status %+v", st)
	}
}

// With slot A set to something other than a GCI folder, GameCube saves don't sync, and say why.
func TestGCSlotANotAFolder(t *testing.T) {
	f := newSaveFixture(t)
	f.configs["dolphin-gc"] = domain.SaveConfig{Problem: "Dolphin's memory card in slot A isn't a GCI folder"}
	f.gcGame(t, 90, "Super Mario Sunshine (USA).rvz", gcDisc("GMSE01", 0))
	if st := f.statusOf(t, 90); st.Syncs || !strings.Contains(st.Why, "slot A") {
		t.Fatalf("status %+v", st)
	}
}

func TestGCRegionDir(t *testing.T) {
	for id, want := range map[string]string{"GMSE01": "USA", "GMSP01": "EUR", "GMSJ01": "JAP"} {
		if got := domain.GCRegionDir(domain.DolphinDisc{ID: id}); got != want {
			t.Errorf("%s: %s", id, got)
		}
	}
}

// A GameCube card problem shows Dolphin's sync as paused, saying why, though Wii saves still sync.
func TestGCSlotAPausesDolphin(t *testing.T) {
	f := newSaveFixture(t)
	f.configs["dolphin-gc"] = domain.SaveConfig{Problem: "Dolphin's memory card in slot A isn't a GCI folder"}
	f.saves = f.newSaves()
	for _, p := range f.saves.Paused() {
		if p.Emulator.ID == "dolphin" && strings.Contains(p.Why, "slot A") {
			return
		}
	}
	t.Fatalf("paused: %+v", f.saves.Paused())
}
