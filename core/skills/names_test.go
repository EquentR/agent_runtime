package skills

import "testing"

func TestNormalizeNamesTrimsDedupesAndDropsEmpty(t *testing.T) {
	got := NormalizeNames([]string{" debug ", "", "debug", " review "})
	want := []string{"debug", "review"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
