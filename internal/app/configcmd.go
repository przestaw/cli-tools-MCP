// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"workctl/internal/cliutil"
	"workctl/internal/config"
)

const configUsage = "usage: workctl config show|path|set KEY VALUE|validate [--json]"

// configFile is the file `config` works on: --config, or the default path.
func configFile(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return config.DefaultPath()
}

// configSet is `workctl config set KEY VALUE`: change one policy, timeout or default setting and save.
func configSet(path string, args []string) error {
	if len(args) != 2 {
		return errors.New(configUsage)
	}
	cfg, err := config.LoadOrDefault(path)
	if err != nil {
		return err
	}
	if err := cfg.Set(args[0], args[1]); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("saved: %s\n", cfg.Path())
	return nil
}

// configValidate is `workctl config validate [--json]`: report every problem of the file and fail when there is one.
func configValidate(path string, args []string) error {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the result as one JSON document")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := configFile(path)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err == nil {
		err = cfg.Validate()
	}
	problems := []string{}
	for _, p := range config.Problems(err) {
		problems = append(problems, p.Error())
	}
	if *asJSON {
		b, _ := json.Marshal(map[string]any{"ok": err == nil, "path": path, "problems": problems})
		_ = cliutil.PrintBody(b)
	} else if err == nil {
		fmt.Printf("%s is valid\n", path)
	} else {
		for _, p := range problems {
			fmt.Println("problem:", p)
		}
	}
	if err != nil {
		return fmt.Errorf("%s has %d problem(s)", path, len(problems))
	}
	return nil
}
