package recency

import (
	"testing"
)

func TestRecordMergesSuccessfulSelections(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	if err := Record(map[string]string{"identity": "work", "project": "one"}); err != nil {
		t.Fatal(err)
	}
	first := Load().Time("identity", "work")
	if first.IsZero() {
		t.Fatal("identity use was not recorded")
	}
	if err := Record(map[string]string{"project": "two", "docker": "local"}); err != nil {
		t.Fatal(err)
	}
	history := Load()
	for kind, name := range map[string]string{"identity": "work", "project": "two", "docker": "local"} {
		if history.Time(kind, name).IsZero() {
			t.Errorf("missing %s %q", kind, name)
		}
	}
}
