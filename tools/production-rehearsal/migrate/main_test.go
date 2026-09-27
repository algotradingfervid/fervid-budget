package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Exercise the actual subprocess interface used by prepare_local.py. A newline
// accidentally added by the caller must not silently become a different login.
func TestSyntheticPasswordSubprocessExactBytes(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "rehearsal-helper")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, output)
	}
	password := "Synthetic-Only-Password-42"
	cmd := exec.Command(bin, "--hash-password")
	cmd.Stdin = strings.NewReader(password)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("hash subprocess: %v", err)
	}
	hash := bytes.TrimSpace(output)
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil {
		t.Fatal("subprocess hash does not match the exact synthetic credential")
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password+"\n")) == nil {
		t.Fatal("hash unexpectedly accepts a changed credential")
	}
}

func TestProductionAndBaselinePathsRejected(t *testing.T) {
	for _, path := range []string{"/opt/fervid-budget/data/fervid.db", filepath.Join(root, "baseline/fervid.db"), root + "/migrated/data/../data/fervid.db"} {
		if err := validate(path); err == nil {
			t.Fatal("non-allowlisted path accepted")
		}
	}
}
