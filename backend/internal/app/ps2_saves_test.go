package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

// fakePS2Discs reads SYSTEM.CNF by the disc file's name.
type fakePS2Discs map[string]string

func (d fakePS2Discs) SystemCNF(path string) ([]byte, error) {
	cnf, ok := d[filepath.Base(path)]
	if !ok {
		return nil, errors.New("not a PS2 disc")
	}
	return []byte(cnf), nil
}

var pcsx2 = domain.GameLauncher{
	Label: "PCSX2 (Standalone)", Command: "/usr/bin/bash /home/deck/Emulation/tools/launchers/pcsx2-qt.sh -batch %ROM%", Known: true}

// ps2Game records the PS2 game in file, booting serial's executable, as ROM id on platform 15,
// downloaded by the plugin and played in PCSX2, and returns its folder memory card.
func (f *saveFixture) ps2Game(t *testing.T, id int, file, boot string) string {
	t.Helper()
	card := filepath.Join(f.root, "saves", "pcsx2", "saves", "Mcd001.ps2")
	if err := os.MkdirAll(card, 0o755); err != nil {
		t.Fatal(err)
	}
	f.launchers["ps2"] = pcsx2
	f.configs["pcsx2"] = domain.SaveConfig{Dir: card}
	f.ps2[file] = "BOOT2 = cdrom0:\\" + boot + ";1\r\nVER = 1.00\r\nVMODE = NTSC\r\n"
	f.saves = f.newSaves()
	path := filepath.Join(f.root, "roms", "ps2", file)
	writeFile(t, path, []byte("disc"))
	f.catalog.mu.Lock()
	f.catalog.roms[id] = domain.ROM{ID: id, PlatformID: 15, Name: strings.TrimSuffix(file, filepath.Ext(file)), FileName: file}
	f.catalog.mu.Unlock()
	if err := f.owned.SaveOwned(context.Background(), domain.OwnedROM{ROMID: id, PlatformID: 15, Files: []domain.PlacedFile{{Path: path}}}); err != nil {
		t.Fatal(err)
	}
	return card
}

// jakSave writes a save of Jak and Daxter's as PCSX2 does, in its folder on the card.
func jakSave(t *testing.T, card, bank string) string {
	t.Helper()
	dir := filepath.Join(card, "BASCUS-97124AYBABTU!")
	writeFile(t, filepath.Join(dir, "bank0.bin"), []byte(bank))
	writeFile(t, filepath.Join(dir, "icon.sys"), []byte("icon"))
	writeFile(t, filepath.Join(dir, "_pcsx2_index"), []byte("index"))
	return dir
}

// Only the game's own folders sync; the card's files and other games' folders are left alone.
func TestPS2SavesRoundTrip(t *testing.T) {
	f := newSaveFixture(t)
	card := f.ps2Game(t, 100, "Jak and Daxter (USA).iso", "SCUS_971.24")
	if st := f.statusOf(t, 100); !st.Syncs || st.Path != filepath.Join(card, "SCUS-97124.zip") || st.Emulator != "pcsx2" || st.Saver != "PCSX2" {
		t.Fatalf("status %+v", st)
	}
	writeFile(t, filepath.Join(card, "_pcsx2_superblock"), []byte("superblock"))
	writeFile(t, filepath.Join(card, "BASLUS-20946GTASA", "save.bin"), []byte("grove street"))
	dir := jakSave(t, card, "cell 1")
	if s := f.syncGame(t, 100); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}
	head, _ := f.romm.head(100)
	m := zipMembers(t, head.data)
	if len(m) != 3 || string(m["BASCUS-97124AYBABTU!/bank0.bin"]) != "cell 1" || head.FileName != "SCUS-97124.zip" || head.Emulator != "pcsx2" {
		t.Fatalf("RomM holds %s: %d members", head.FileName, len(m))
	}

	f.romm.otherDevice(100, "pcsx2", domain.FolderUnit(map[string][]byte{
		"BASCUS-97124AYBABTU!/bank0.bin": []byte("cell 101"), "BASCUS-97124AYBABTU!/icon.sys": []byte("icon"),
	}))
	if s := f.syncGame(t, 100); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	if got := readFile(t, filepath.Join(dir, "bank0.bin")); got != "cell 101" {
		t.Fatalf("bank0.bin holds %q", got)
	}
	gone(t, filepath.Join(dir, "_pcsx2_index"))
	if got := readFile(t, filepath.Join(card, "BASLUS-20946GTASA", "save.bin")); got != "grove street" {
		t.Fatalf("another game's save changed: %q", got)
	}
	if got := readFile(t, filepath.Join(card, "_pcsx2_superblock")); got != "superblock" {
		t.Fatalf("the card's superblock changed: %q", got)
	}
	if s := f.syncGame(t, 100); s.Uploaded+s.Downloaded+s.Conflicts != 0 {
		t.Fatalf("second sync %+v", s)
	}
}

// A save folder RomM's save lacks is removed from the card with its files.
func TestPS2DownloadRemovesFoldersItLacks(t *testing.T) {
	f := newSaveFixture(t)
	card := f.ps2Game(t, 100, "Jak and Daxter (USA).iso", "SCUS_971.24")
	writeFile(t, filepath.Join(card, "BASCUS-97124OLD", "bank0.bin"), []byte("old slot"))
	if s := f.syncGame(t, 100); s.Uploaded != 1 {
		t.Fatalf("upload %+v", s)
	}
	f.romm.otherDevice(100, "pcsx2", domain.FolderUnit(map[string][]byte{"BASCUS-97124AYBABTU!/bank0.bin": []byte("new")}))
	if s := f.syncGame(t, 100); s.Downloaded != 1 {
		t.Fatalf("download %+v", s)
	}
	gone(t, filepath.Join(card, "BASCUS-97124OLD"))
	if got := readFile(t, filepath.Join(card, "BASCUS-97124AYBABTU!", "bank0.bin")); got != "new" {
		t.Fatalf("bank0.bin holds %q", got)
	}
}

// A save from RomM with another game's folder, or a file loose on the card, never lands on it.
func TestPS2SaveRefusesOtherGamesFolders(t *testing.T) {
	for name, member := range map[string]string{"another game": "BASLUS-20946GTASA/save.bin", "the card's own": "_pcsx2_superblock"} {
		t.Run(name, func(t *testing.T) {
			f := newSaveFixture(t)
			card := f.ps2Game(t, 100, "Jak and Daxter (USA).iso", "SCUS_971.24")
			f.romm.otherDevice(100, "pcsx2", domain.FolderUnit(map[string][]byte{member: []byte("not ours")}))
			f.syncGame(t, 100)
			gone(t, filepath.Join(card, filepath.FromSlash(member)))
			if st := f.statusOf(t, 100); !strings.Contains(st.Problem, "PCSX2 can't use it") {
				t.Fatalf("status %+v", st)
			}
		})
	}
}

// The watch reports files in the game's folders, and the folders as the game makes them.
func TestPS2SaveOwns(t *testing.T) {
	p := ps2Save{card: "/card", serial: "SCUS-97124"}
	for path, want := range map[string]bool{
		"/card/BASCUS-97124AYBABTU!":           true,
		"/card/BASCUS-97124AYBABTU!/bank0.bin": true,
		"/card/BASLUS-20946GTASA/save.bin":     false,
		"/card/_pcsx2_superblock":              false,
		"/elsewhere/BASCUS-97124X/bank0.bin":   false,
	} {
		if got := formatOwns(p, path); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
}
