package xxmi

import (
	"reflect"
	"testing"
)

func TestINIOptimizerExclusions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ini  string
		want []string
	}{
		{
			name: "repeated options",
			ini:  "\ufeff[Include]\r\nexclude_recursive = DISABLED*\r\nexclude_recursive = desktop.ini ; Windows metadata\r\n",
			want: []string{"DISABLED*", "desktop.ini"},
		},
		{
			name: "other sections and comments",
			ini:  "[Other]\nexclude_recursive = ignored.ini\n[Include]\n; exclude_recursive = ignored2.ini\n",
			want: []string{"DISABLED*"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := iniOptimizerExclusions([]byte(test.ini)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("exclusions = %q, want %q", got, test.want)
			}
		})
	}
}
