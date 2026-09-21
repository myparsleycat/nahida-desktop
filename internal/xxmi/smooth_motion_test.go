package xxmi

import "testing"

func TestSmoothMotionApplied(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		sample  smoothMotionSample
		enabled bool
	}{
		{name: "unset everywhere"},
		{
			name:   "global off",
			sample: smoothMotionSample{global: storedDword{present: true, explicit: true}},
		},
		{
			name:    "global on and no game profile",
			sample:  smoothMotionSample{global: storedDword{present: true, explicit: true, value: 1}},
			enabled: true,
		},
		{
			name: "game profile without an explicit value follows global",
			sample: smoothMotionSample{
				profileFound: true,
				global:       storedDword{present: true, explicit: true, value: 1},
			},
			enabled: true,
		},
		{
			name: "inherited game value does not override the global read",
			sample: smoothMotionSample{
				profileFound: true,
				profile:      storedDword{present: true, value: 1},
				global:       storedDword{present: true, explicit: true},
			},
		},
		{
			name: "explicit game off wins over global on",
			sample: smoothMotionSample{
				profileFound: true,
				profile:      storedDword{present: true, explicit: true},
				global:       storedDword{present: true, explicit: true, value: 1},
			},
		},
		{
			name: "explicit game on",
			sample: smoothMotionSample{
				profileFound: true,
				profile:      storedDword{present: true, explicit: true, value: 1},
			},
			enabled: true,
		},
		{
			name: "non-zero explicit value is on",
			sample: smoothMotionSample{
				profileFound: true,
				profile:      storedDword{present: true, explicit: true, value: 2},
			},
			enabled: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := smoothMotionApplied(tc.sample); got != tc.enabled {
				t.Fatalf("smoothMotionApplied = %v, want %v", got, tc.enabled)
			}
		})
	}
}
