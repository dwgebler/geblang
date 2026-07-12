package wslpath

import "testing"

func TestUNCToLinux(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`\\wsl.localhost\Ubuntu\home\daveg\main.gb`, "/home/daveg/main.gb", true},
		{`\\wsl$\Ubuntu\home\daveg\main.gb`, "/home/daveg/main.gb", true},
		{`\\WSL.LOCALHOST\Ubuntu\home\daveg\main.gb`, "/home/daveg/main.gb", true},
		{`//wsl.localhost/Ubuntu/home/daveg/main.gb`, "/home/daveg/main.gb", true},
		{`\\wsl.localhost\Ubuntu`, "", false},
		{`\\server\share\file.gb`, "", false},
		{`/home/daveg/main.gb`, "", false},
		{`C:\Users\daveg\main.gb`, "", false},
	}
	for _, c := range cases {
		got, ok := UNCToLinux(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("UNCToLinux(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestDriveToWSL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`C:\Users\daveg\main.gb`, "/mnt/c/Users/daveg/main.gb", true},
		{`d:\proj\a.gb`, "/mnt/d/proj/a.gb", true},
		{`C:`, "/mnt/c", true},
		{`C:/mixed/slash.gb`, "/mnt/c/mixed/slash.gb", true},
		{`/home/daveg/main.gb`, "", false},
		{`\\wsl.localhost\Ubuntu\home\x.gb`, "", false},
	}
	for _, c := range cases {
		got, ok := DriveToWSL(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("DriveToWSL(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLooksLikeWindowsDrive(t *testing.T) {
	if !LooksLikeWindowsDrive("C:\\x") || LooksLikeWindowsDrive("/home") || LooksLikeWindowsDrive("1:x") {
		t.Error("LooksLikeWindowsDrive misclassified an input")
	}
}
