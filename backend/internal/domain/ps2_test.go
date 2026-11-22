package domain

import "testing"

func TestPS2Serial(t *testing.T) {
	for cnf, want := range map[string]string{
		"BOOT2 = cdrom0:\\SCUS_971.24;1\r\nVER = 1.00\r\n": "SCUS-97124",
		"BOOT2=cdrom0:\\SLUS_209.46;1\n":                   "SLUS-20946",
		"boot2 = CDROM0:\\DATA\\SLES_524.60;1":             "SLES-52460",
		"BOOT = cdrom:\\SLUS_007.13;1":                     "",
		"VER = 1.00":                                       "",
	} {
		if got := PS2Serial([]byte(cnf)); got != want {
			t.Errorf("%q: %q", cnf, got)
		}
	}
}

func TestPS2SaveOf(t *testing.T) {
	for name, want := range map[string]bool{
		"BASCUS-97124AYBABTU!": true,
		"BESCUS-97124":         true,
		"BASLUS-20946GTASA":    false,
		"_pcsx2_superblock":    false,
		"BA":                   false,
	} {
		if got := PS2SaveOf(name, "SCUS-97124"); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
