package log

import (
	"bufio"
	"os"
	"testing"
)

func TestDebugLevel(t *testing.T) {
	test := "/tmp/levels.log"
	os.Remove(test)
	SetLogger(DEBUG, test, 10000)
	Debug("test Debug")
	Info("test Info")
	Error("test Error")

	f, err := os.Open(test)
	if err != nil {
		t.Error("the logger file:", test, "was not generated, or can't be accessed")
		t.Fail()
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	l := 0
	for scanner.Scan() {
		l++
	}

	if l != 3 {
		t.Error("expected lines: 3, but:", l, "obtained")
	}
}

func TestInfoLevel(t *testing.T) {
	test := "/tmp/levels.log"
	os.Remove(test)
	SetLogger(INFO, test, 10000)
	Debug("test Debug")
	Info("test Info")
	Error("test Error")

	f, err := os.Open(test)
	if err != nil {
		t.Error("the logger file:", test, "was not generated, or can't be accessed")
		t.Fail()
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	l := 0
	for scanner.Scan() {
		l++
	}

	if l != 2 {
		t.Error("Expected lines: 2, but:", l, "obtained")
	}
}

func TestErrorLevel(t *testing.T) {
	test := "/tmp/levels.log"
	os.Remove(test)
	SetLogger(ERROR, test, 10000)
	Debug("test Debug")
	Info("test Info")
	Error("test Error")

	f, err := os.Open(test)
	if err != nil {
		t.Error("the logger file:", test, "was not generated, or can't be accessed")
		t.Fail()
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	l := 0
	for scanner.Scan() {
		l++
	}

	if l != 1 {
		t.Error("Expected lines: 1, but:", l, "obtained")
	}
}
