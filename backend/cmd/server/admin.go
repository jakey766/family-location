package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"family-location/backend/internal/auth"
	"family-location/backend/internal/config"
	"family-location/backend/internal/storage"
	"golang.org/x/term"
)

func initAdmin(ctx context.Context, args []string, input *os.File, output io.Writer) error {
	restore, err := guardTerminal(input)
	if err != nil {
		return err
	}
	defer restore()
	flags := flag.NewFlagSet("init-admin", flag.ContinueOnError)
	flags.SetOutput(output)
	username := flags.String("username", "", "管理员用户名（3–64 位字母、数字、点、下划线或连字符）")
	family := flags.String("family", "", "家庭名称")
	passwordStdin := flags.Bool("password-stdin", false, "从标准输入读取密码，必须同时提供 username 和 family")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("init-admin 不接受位置参数")
	}
	var password []byte
	if *passwordStdin {
		if *username == "" || *family == "" {
			return fmt.Errorf("password-stdin 必须同时指定 username 和 family")
		}
		password, err = readPasswordStdin(input)
	} else {
		if !term.IsTerminal(int(input.Fd())) {
			return fmt.Errorf("需要交互终端；自动化请显式使用 --password-stdin")
		}
		reader := bufio.NewReader(input)
		if *username == "" {
			*username, err = promptLine(reader, output, "管理员用户名: ")
			if err != nil {
				return err
			}
		}
		if *family == "" {
			*family, err = promptLine(reader, output, "家庭名称: ")
			if err != nil {
				return err
			}
		}
		fmt.Fprint(output, "管理员密码（12–128 字节，输入不回显）: ")
		password, err = term.ReadPassword(int(input.Fd()))
		fmt.Fprintln(output)
		if err == nil {
			fmt.Fprint(output, "再次输入密码: ")
			confirm, readErr := term.ReadPassword(int(input.Fd()))
			fmt.Fprintln(output)
			if readErr != nil {
				err = readErr
			} else if string(password) != string(confirm) {
				err = fmt.Errorf("两次密码不一致")
			}
			clear(confirm)
		}
	}
	defer clear(password)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	admin, err := auth.New(db).CreateAdmin(ctx, *username, string(password), *family)
	if err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}
	fmt.Fprintf(output, "管理员 %s 已创建，家庭 ID: %d。请使用登录接口验证。\n", admin.Username, admin.FamilyID)
	return nil
}

func promptLine(reader *bufio.Reader, output io.Writer, prompt string) (string, error) {
	fmt.Fprint(output, prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func readPasswordStdin(input io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(input, 131))
	if err != nil {
		return nil, fmt.Errorf("读取密码失败")
	}
	s := strings.TrimSuffix(string(b), "\n")
	s = strings.TrimSuffix(s, "\r")
	clear(b)
	if len(s) == 0 || len(s) > 128 || strings.ContainsAny(s, "\r\n") {
		return nil, fmt.Errorf("标准输入必须仅包含一行密码，最多 128 字节")
	}
	return []byte(s), nil
}
