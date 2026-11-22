package domain

import (
	"encoding/binary"
	"path/filepath"
	"testing"
)

func ctrHeader(magic string, at int, id uint64) []byte {
	h := make([]byte, CTRHeaderSize)
	copy(h[0x100:], magic)
	binary.LittleEndian.PutUint64(h[at:], id)
	return h
}

func TestParseCTRTitleID(t *testing.T) {
	cases := map[string]struct {
		header []byte
		want   uint64
		ok     bool
	}{
		"cartridge image": {ctrHeader("NCSD", 0x108, 0x0004000000054000), 0x0004000000054000, true},
		"executable":      {ctrHeader("NCCH", 0x118, 0x00040000000EC300), 0x00040000000EC300, true},
		"something else":  {ctrHeader("3DSX", 0x108, 1), 0, false},
		"too short":       {make([]byte, 0x100), 0, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseCTRTitleID(c.header)
			if (err == nil) != c.ok || got != c.want {
				t.Fatalf("got %016X, %v", got, err)
			}
		})
	}
}

func TestCTRTitleDir(t *testing.T) {
	zeros := "00000000000000000000000000000000"
	want := filepath.Join("/sd", "Nintendo 3DS", zeros, zeros, "title", "00040000", "00054000")
	if got := CTRTitleDir("/sd", 0x0004000000054000); got != want {
		t.Fatalf("got %s", got)
	}
	if got := CTRSaveName(0x0004000000054000); got != "0004000000054000.zip" {
		t.Fatalf("got %s", got)
	}
}

func TestFolderUnitRoundTrip(t *testing.T) {
	files := map[string][]byte{"00000001/GameData.bin": []byte("save"), "00000001.metadata": []byte("meta")}
	unit := FolderUnit(files)
	// The same files always zip to the same bytes, so the hash RomM keeps doesn't drift.
	if string(unit) != string(FolderUnit(files)) {
		t.Fatal("unit isn't deterministic")
	}
	got, err := ParseFolderUnit(unit)
	if err != nil || len(got) != 2 || string(got["00000001/GameData.bin"]) != "save" || string(got["00000001.metadata"]) != "meta" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseFolderUnitRefusesEscapes(t *testing.T) {
	for _, name := range []string{"../evil", "/etc/passwd", `..\evil`, "a/../../evil"} {
		if _, err := ParseFolderUnit(FolderUnit(map[string][]byte{name: []byte("x")})); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
	if _, err := ParseFolderUnit([]byte("not a zip")); err == nil {
		t.Error("a raw file was accepted")
	}
}

func TestAzaharRuleMatchesEmuDecksLauncher(t *testing.T) {
	for _, command := range []string{
		"%EMULATOR_AZAHAR% %ROM%",
		"/usr/bin/bash /home/deck/Emulation/tools/launchers/azahar.sh %ROM%",
	} {
		r, ok := GameLauncher{ESDECommand: ESDECommand{Label: "Azahar (Standalone)", Command: command}, Known: true}.SaveRule()
		if !ok || r.Emulator.ID != "azahar" || r.Kind != SaveCTRTitle {
			t.Errorf("%q: %+v %v", command, r, ok)
		}
	}
}
