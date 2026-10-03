//go:build windows

package main

import "testing"

func TestWindowsDriveLetterParsing(t *testing.T) {
	for _, input := range []string{"D:", "d:", "D:\\", "d:/", "\"E:\\\""} {
		letter, ok := windowsDriveLetter(input)
		if !ok {
			t.Fatalf("expected %q to parse", input)
		}
		if input[0] == 'E' && letter != 'E' {
			t.Fatalf("got %c for %q", letter, input)
		}
	}
	for _, input := range []string{"", "DVD", "D:\\folder", "1:", "/dev/sr0"} {
		if _, ok := windowsDriveLetter(input); ok {
			t.Fatalf("did not expect %q to parse as a Windows drive root", input)
		}
	}
}
