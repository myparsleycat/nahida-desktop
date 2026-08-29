package mod

import "testing"

func TestReadGameBananaModID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		want    int64
		wantNil bool
	}{
		{`{"source":"gamebanana","mod":{"id":123}}`, 123, false},
		{`{"source":"mod"}`, 0, true},
		{`{"source":"gamebanana","mod":{"id":-1}}`, 0, true},
		{`{"source":"gamebanana","mod":{"id":"123"}}`, 0, true},
	}
	for _, test := range cases {
		got := decodeGameBananaModID([]byte(test.raw))
		if test.wantNil {
			if got != nil {
				t.Fatalf("%s: got %v, want nil", test.raw, *got)
			}
			continue
		}
		if got == nil || *got != test.want {
			t.Fatalf("%s: got %v, want %d", test.raw, got, test.want)
		}
	}
}
