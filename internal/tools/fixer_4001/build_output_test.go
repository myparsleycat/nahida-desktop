package fixer4001

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsMissingSDKHeaderError(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "ucrt header", text: `yvals.h(20,10): error C1083: cannot open include file: 'crtdbg.h'`, want: true},
		{name: "windows header", text: `pch.h(69,10): error C1083: 포함 파일을 열 수 없습니다. 'windows.h'`, want: true},
		{name: "shared header", text: `targetver.h(8,10): error C1083: 'SDKDDKVer.h'`, want: true},
		{name: "localized quotes", text: "pch.h(69,10): fatal error C1083: 无法打开包括文件: “windows.h”", want: true},
		{
			name: "console code page quotes",
			text: "pch.h(69,10): fatal error C1083: \xa1\xb0windows.h\xa1\xb1",
			want: true,
		},
		{
			name: "msbuild SDK version",
			text: `error MSB8036: The Windows SDK version 10.0.22621.0 was not found.`,
			want: true,
		},
		{name: "similar header name", text: `main.cpp(1,10): error C1083: 'mywindows.h'`},
		{name: "project header", text: `main.cpp(1,10): error C1083: 'myproject.h'`},
		{name: "other compiler error", text: `main.cpp(1,10): error C1001: 'windows.h'`},
		{name: "unrelated text", text: `check windows.h and crtdbg.h`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMissingSDKHeaderError(errors.New(tc.text)); got != tc.want {
				t.Fatalf("isMissingSDKHeaderError(%q) = %t, want %t", tc.text, got, tc.want)
			}
		})
	}
	if isMissingSDKHeaderError(nil) {
		t.Fatal("nil error classified as missing SDK")
	}
}

func TestExtractBuildErrorMessagePrefersFirstTwelveCompilerErrors(t *testing.T) {
	lines := []string{"Build failed: exit status 1", "stdout:"}
	for index := range 14 {
		lines = append(lines, fmt.Sprintf("file.cpp(%d): error C%04d: failure %d", index+1, 1000+index, index+1))
	}
	got := extractBuildErrorMessage(errors.New(strings.Join(lines, "\r\n")))
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 12 || !strings.Contains(gotLines[0], "C1000") || !strings.Contains(gotLines[11], "C1011") {
		t.Fatalf("digest = %q", got)
	}
}

func TestExtractBuildErrorMessageFallsBackToLastTwelveNonemptyLines(t *testing.T) {
	lines := make([]string, 15)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %d", index+1)
	}
	got := extractBuildErrorMessage(errors.New(strings.Join(lines, "\n")))
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 12 || gotLines[0] != "line 4" || gotLines[11] != "line 15" {
		t.Fatalf("fallback = %q", got)
	}
}
