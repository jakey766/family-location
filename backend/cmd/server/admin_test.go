package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"family-location/backend/internal/auth"
	"family-location/backend/internal/storage"
)

func TestReadPasswordStdin(t *testing.T) {
	for _, input := range []string{"test-password-only!\n", "test-password-only!\r\n", "test-password-only!"} {
		got, err := readPasswordStdin(strings.NewReader(input))
		if err != nil || string(got) != "test-password-only!" {
			t.Fatalf("password read failed: %v", err)
		}
	}
	for _, input := range []string{"", strings.Repeat("a", 129), "test-password-only!\nextra"} {
		if _, err := readPasswordStdin(strings.NewReader(input)); err == nil {
			t.Fatal("invalid password input accepted")
		}
	}
}

func TestInitAdminCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.sqlite")
	t.Setenv("FAMILY_DB_PATH", path)
	t.Setenv("FAMILY_HTTP_ADDR", ":8080")
	inputPath := filepath.Join(dir, "stdin")
	const password = "test-password-only!"
	if err := os.WriteFile(inputPath, []byte(password+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(args []string) (string, error) {
		in, err := os.Open(inputPath)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		var out bytes.Buffer
		err = initAdmin(context.Background(), args, in, &out)
		if strings.Contains(out.String(), password) {
			t.Fatal("password leaked in command output")
		}
		return out.String(), err
	}
	args := []string{"--username", "owner", "--family", "我的家庭", "--password-stdin"}
	if _, err := invoke(args); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(args); err == nil {
		t.Fatal("duplicate admin accepted")
	}
	if _, err := invoke([]string{"--password-stdin"}); err == nil {
		t.Fatal("missing flags accepted")
	}
	if _, err := invoke([]string{"--username", "second", "--family", "家"}); err == nil {
		t.Fatal("non-terminal password implicitly accepted")
	}
	db, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := auth.New(db).Login(context.Background(), "owner", password, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
}
