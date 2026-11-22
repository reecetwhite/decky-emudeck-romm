package emudeck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func TestMGBASavePlayer(t *testing.T) {
	cases := map[string]struct {
		config  string
		problem bool
	}{
		"no config":   {"", false},
		"no player":   {"[ports.qt]\n", false},
		"player 1":    {"[ports.qt]\nsavePlayerId=1\n", false},
		"player 3":    {"[ports.qt]\nsavePlayerId=3\n", true},
		"elsewhere 3": {"[gba]\nsavePlayerId=3\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if c.config != "" {
				put(t, filepath.Join(home, mGBAConfig), c.config)
			}
			got := NewSaveConfigs(home).SaveConfig("mgba").Problem
			if (got != "") != c.problem || c.problem && !strings.Contains(got, ".sa3") {
				t.Fatalf("got %q", got)
			}
		})
	}
	if got := NewSaveConfigs(t.TempDir()).SaveConfig("melonds"); got != (domain.SaveConfig{}) {
		t.Fatalf("melonDS: %+v", got)
	}
}

// Each emulator's saves folder setting is followed through links; one set to nothing, or to a
// folder that isn't there, stops its saves from syncing.
func TestSaveFolderSettings(t *testing.T) {
	type file struct{ path, body string }
	cases := map[string]struct {
		id     string
		config func(saves string) file
		// dir is where the saves are, relative to the test's saves folder; empty for the problem.
		dir     string
		problem string
	}{
		"RetroArch": {"retroarch", func(saves string) file {
			return file{retroArchConfig, "savefile_directory = \"" + saves + "/retroarch/saves\"\n"}
		}, "retroarch/saves", ""},
		"RetroArch unset": {"retroarch", func(string) file {
			return file{retroArchConfig, "savefile_directory = \"default\"\n"}
		}, "", "Save Files"},
		"RetroArch beside games": {"retroarch", func(saves string) file {
			return file{retroArchConfig, "savefile_directory = \"" + saves + "\"\nsavefiles_in_content_dir = \"true\"\n"}
		}, "", "Content Directory"},
		"RetroArch sorted": {"retroarch", func(saves string) file {
			return file{retroArchConfig, "savefile_directory = \"" + saves + "\"\nsort_savefiles_enable = \"true\"\n"}
		}, "", "Sort Saves"},
		"melonDS": {"melonds", func(saves string) file {
			return file{melonDSConfig + "/melonDS.toml", "[Instance0]\nSaveFilePath = \"" + saves + "/melonds/saves\"\n"}
		}, "melonds/saves", ""},
		"melonDS ini": {"melonds", func(saves string) file {
			return file{melonDSConfig + "/melonDS.ini", "SaveFilePath=" + saves + "/melonds/saves\n"}
		}, "melonds/saves", ""},
		"melonDS beside games": {"melonds", func(string) file {
			return file{melonDSConfig + "/melonDS.toml", "[Instance0]\nSaveFilePath = \"\"\n"}
		}, "", "Path settings"},
		"mGBA": {"mgba", func(saves string) file {
			return file{mGBAConfig, "[ports.qt]\nsavegamePath=" + saves + "/mgba/saves\n"}
		}, "mgba/saves", ""},
		"mGBA moved away": {"mgba", func(saves string) file {
			return file{mGBAConfig, "[ports.qt]\nsavegamePath=" + saves + "/gone\n"}
		}, "", "isn't there"},
		"RMG": {"rmg", func(saves string) file {
			return file{rmgConfig, "[Core]\nSaveSRAMPath = \"" + saves + "/RMG/saves\"\n"}
		}, "RMG/saves", ""},
		"RMG inside its app": {"rmg", func(string) file {
			return file{rmgConfig, "[Core]\nSaveSRAMPath = \"\"\n"}
		}, "", "SRAM"},
		"DuckStation": {"duckstation", func(saves string) file {
			return file{duckStationData + "/settings.ini", "[MemoryCards]\nDirectory = " + saves + "/duckstation/saves\n"}
		}, "duckstation/saves", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			saves := filepath.Join(home, "Emulation", "saves")
			for _, d := range []string{"retroarch/saves", "melonds/saves", "mgba/saves", "RMG/saves", "duckstation/saves"} {
				if err := os.MkdirAll(filepath.Join(saves, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			f := c.config(saves)
			put(t, filepath.Join(home, f.path), f.body)
			got := NewSaveConfigs(home).SaveConfig(c.id)
			if c.problem != "" {
				if got.Dir != "" || !strings.Contains(got.Problem, c.problem) {
					t.Fatalf("got %+v", got)
				}
				return
			}
			want, _ := filepath.EvalSymlinks(filepath.Join(saves, c.dir))
			if got.Problem != "" || got.Dir != want {
				t.Fatalf("got %+v, want %s", got, want)
			}
		})
	}
}

// RetroArch's and the mGBA app's save states folder settings are followed like their saves'; ones
// that put states next to the games or sort them into folders stop them syncing.
func TestStateFolderSettings(t *testing.T) {
	cases := map[string]struct {
		id, path, body string
		// dir is where the states are, relative to the test's saves folder; empty for the problem.
		dir     string
		problem string
	}{
		"no settings":         {"retroarch", "", "", "", ""},
		"RetroArch":           {"retroarch", retroArchConfig, "savestate_directory = \"SAVES/retroarch/states\"\n", "retroarch/states", ""},
		"RetroArch unset":     {"retroarch", retroArchConfig, "savestate_directory = \"default\"\n", "", "Save States"},
		"RetroArch in games":  {"retroarch", retroArchConfig, "savestate_directory = \"SAVES/retroarch/states\"\nsavestates_in_content_dir = \"true\"\n", "", "Content Directory"},
		"RetroArch sorted":    {"retroarch", retroArchConfig, "savestate_directory = \"SAVES/retroarch/states\"\nsort_savestates_by_content_enable = \"true\"\n", "", "Sort Save States"},
		"RetroArch gone":      {"retroarch", retroArchConfig, "savestate_directory = \"SAVES/gone\"\n", "", "save states in"},
		"mGBA":                {"mgba", mGBAConfig, "[ports.qt]\nsavestatePath=SAVES/mgba/states\n", "mgba/states", ""},
		"mGBA beside games":   {"mgba", mGBAConfig, "[ports.qt]\nsavestatePath=\n", "", "Paths"},
		"saves stay separate": {"mgba", mGBAConfig, "[ports.qt]\nsavegamePath=SAVES/mgba/saves\n", "", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			saves := filepath.Join(home, "Emulation", "saves")
			for _, d := range []string{"retroarch/states", "mgba/states", "mgba/saves"} {
				if err := os.MkdirAll(filepath.Join(saves, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if c.path != "" {
				put(t, filepath.Join(home, c.path), strings.ReplaceAll(c.body, "SAVES", saves))
			}
			got := NewSaveConfigs(home).StateConfig(c.id)
			switch {
			case c.problem != "":
				if got.Dir != "" || !strings.Contains(got.Problem, c.problem) {
					t.Fatalf("got %+v", got)
				}
			case c.dir == "":
				if got != (domain.SaveConfig{}) {
					t.Fatalf("got %+v, want EmuDeck's folder to stand", got)
				}
			default:
				want, _ := filepath.EvalSymlinks(filepath.Join(saves, c.dir))
				if got.Problem != "" || got.Dir != want {
					t.Fatalf("got %+v, want %s", got, want)
				}
			}
		})
	}
}

// A saves folder set through a link is followed to where the link leads.
func TestSaveFolderSettingFollowsLinks(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "Emulation", "saves", "mgba")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(native, link); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, mGBAConfig), "[ports.qt]\nsavegamePath="+link+"\n")
	want, _ := filepath.EvalSymlinks(native)
	if got := NewSaveConfigs(home).SaveConfig("mgba"); got.Dir != want {
		t.Fatalf("got %+v", got)
	}
}

// DuckStation keeps its cards under its data folder when its settings name none, or a relative one.
func TestDuckStationCardFolder(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, duckStationData)
	for _, d := range []string{"memcards", "cards"} {
		if err := os.MkdirAll(filepath.Join(data, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for config, dir := range map[string]string{"[MemoryCards]\n": "memcards", "[MemoryCards]\nDirectory = cards\n": "cards"} {
		put(t, filepath.Join(data, "settings.ini"), config)
		want, _ := filepath.EvalSymlinks(filepath.Join(data, dir))
		if got := NewSaveConfigs(home).SaveConfig("duckstation"); got.Dir != want {
			t.Fatalf("%q: got %+v", config, got)
		}
	}
}

func TestSaveConfigFiles(t *testing.T) {
	home := t.TempDir()
	files := NewSaveConfigs(home).Files()
	for _, want := range []string{filepath.Join(home, mGBAConfig), filepath.Join(home, duckStationData, "settings.ini")} {
		found := false
		for _, f := range files {
			found = found || f == want
		}
		if !found {
			t.Fatalf("%s not in %v", want, files)
		}
	}
}

// Azahar's saves are on its SD card: the folder its custom storage names, else its default one, and
// none when it has the SD card turned off.
func TestAzaharSDCard(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "Emulation", "storage", "azahar", "sdmc")
	byDefault := filepath.Join(home, azaharSDMC)
	for _, d := range []string{custom, byDefault} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]struct {
		config  string
		dir     string
		problem bool
	}{
		"custom storage":  {"[Data%20Storage]\nuse_custom_storage=true\nsdmc_directory=" + custom + "/\nuse_virtual_sd=true\n", custom, false},
		"default storage": {"[Data%20Storage]\nuse_custom_storage=false\nsdmc_directory=" + custom + "/\n", byDefault, false},
		"no SD card":      {"[Data%20Storage]\nuse_virtual_sd=false\n", "", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			put(t, filepath.Join(home, azaharConfig), c.config)
			got := NewSaveConfigs(home).SaveConfig("azahar")
			if got.Dir != c.dir || (got.Problem != "") != c.problem {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// Dolphin's Wii NAND is in its user folder, EmuDeck's saves folder standing for it, unless its
// settings move it.
func TestDolphinNAND(t *testing.T) {
	home := t.TempDir()
	if got := NewSaveConfigs(home).SaveConfig("dolphin"); got != (domain.SaveConfig{}) {
		t.Fatalf("no settings: %+v", got)
	}
	nand := filepath.Join(home, "nand")
	if err := os.MkdirAll(nand, 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(home, dolphinConfig), "[General]\nNANDRootPath = "+nand+"\n")
	if got := NewSaveConfigs(home).SaveConfig("dolphin"); got.Dir != nand {
		t.Fatalf("moved NAND: %+v", got)
	}
}

// GameCube saves sync from a GCI folder in slot A: Dolphin's default, or the folder its settings
// name; any other card in slot A stops them.
func TestDolphinGCSlotA(t *testing.T) {
	home := t.TempDir()
	card := filepath.Join(home, "card")
	if err := os.MkdirAll(card, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		config  string
		dir     string
		problem bool
	}{
		"default":    {"[Core]\n", "", false},
		"GCI folder": {"[Core]\nSlotA = 8\n", "", false},
		"raw card":   {"[Core]\nSlotA = 1\n", "", true},
		"own folder": {"[Core]\nSlotA = 8\nGCIFolderAPathOverride = " + card + "\n", card, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			put(t, filepath.Join(home, dolphinConfig), c.config)
			got := NewSaveConfigs(home).SaveConfig("dolphin-gc")
			if got.Dir != c.dir || (got.Problem != "") != c.problem {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// PS2 saves sync from a folder memory card in slot 1; a single-file card, or none, stops them.
func TestPCSX2Card(t *testing.T) {
	home := t.TempDir()
	cards := filepath.Join(home, "memcards")
	folder := filepath.Join(cards, "Mcd001.ps2")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(cards, "Mcd002.ps2"), "a file card")
	ini := filepath.Join(home, pcsx2Data, "inis", "PCSX2.ini")
	if got := NewSaveConfigs(home).SaveConfig("pcsx2"); got.Problem == "" {
		t.Fatalf("no settings: %+v", got)
	}
	cases := map[string]struct {
		config  string
		dir     string
		problem bool
	}{
		"folder card": {"[Folders]\nMemoryCards = " + cards + "\n[MemoryCards]\nSlot1_Enable = true\nSlot1_Filename = Mcd001.ps2\n", folder, false},
		"file card":   {"[Folders]\nMemoryCards = " + cards + "\n[MemoryCards]\nSlot1_Filename = Mcd002.ps2\n", "", true},
		"no slot 1":   {"[MemoryCards]\nSlot1_Enable = false\n", "", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			put(t, ini, c.config)
			got := NewSaveConfigs(home).SaveConfig("pcsx2")
			if got.Dir != c.dir || (got.Problem != "") != c.problem {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// Cemu's saves are in its MLC's usr/save, the MLC its settings name.
func TestCemuMLC(t *testing.T) {
	home := t.TempDir()
	mlc := filepath.Join(home, "Emulation", "roms", "wiiu", "mlc01")
	if err := os.MkdirAll(filepath.Join(mlc, "usr", "save"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := NewSaveConfigs(home).SaveConfig("cemu"); got != (domain.SaveConfig{}) {
		t.Fatalf("no settings: %+v", got)
	}
	put(t, filepath.Join(home, cemuConfig), "<content>\n  <mlc_path>"+mlc+"</mlc_path>\n</content>\n")
	if got := NewSaveConfigs(home).SaveConfig("cemu"); got.Dir != filepath.Join(mlc, "usr", "save") {
		t.Fatalf("got %+v", got)
	}
}
