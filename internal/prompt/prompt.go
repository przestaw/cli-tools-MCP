// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package prompt has the questions the setup wizards ask on the terminal.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// Ask reads a line; an empty answer returns def.
func Ask(r *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// AskBool asks a yes/no question; an empty answer returns def.
func AskBool(r *bufio.Reader, label string, def bool) bool {
	d := "Y/n"
	if !def {
		d = "y/N"
	}
	fmt.Printf("%s [%s]: ", label, d)
	line, _ := r.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes"
}

// AskChoice asks until the answer is one of options (case-insensitive).
func AskChoice(r *bufio.Reader, label string, options []string, def string) string {
	full := fmt.Sprintf("%s (%s)", label, strings.Join(options, "/"))
	for {
		a := strings.ToLower(Ask(r, full, def))
		for _, o := range options {
			if a == strings.ToLower(o) {
				return o
			}
		}
		fmt.Printf("choose one of: %s\n", strings.Join(options, ", "))
	}
}

// AskSecret reads a secret without echo. When stdin is not a terminal (a pipe) it reads one line from r.
func AskSecret(r *bufio.Reader, label string) (string, error) {
	fmt.Printf("%s: ", label)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
