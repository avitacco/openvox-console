package runtime

import "testing"

func TestParsePuppetServerAddresses(t *testing.T) {
	got, err := ParsePuppetServerAddresses(" compile1=https://10.0.0.5:8140/ , compile2=https://c2.internal:8141")
	if err != nil {
		t.Fatalf("ParsePuppetServerAddresses: %v", err)
	}
	if got["compile1"] != "https://10.0.0.5:8140" || got["compile2"] != "https://c2.internal:8141" || len(got) != 2 {
		t.Errorf("got %v", got)
	}
	if got, err := ParsePuppetServerAddresses(""); err != nil || len(got) != 0 {
		t.Errorf("empty: %v, %v", got, err)
	}
	for _, bad := range []string{"compile1", "=https://x:8140", "compile1=http://x:8140", "compile1=x:8140", "a=https://x,a=https://y"} {
		if _, err := ParsePuppetServerAddresses(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
