// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/config"
	"workctl/internal/atlassian/service"
	"workctl/internal/prompt"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage atlassian configuration (profiles, credentials)",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup wizard for a profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetupWizard()
	},
}

// runSetupWizard prompts for connection details, writes the config YAML,
// stores the API token in the keyring, then verifies connectivity. Shared by
// `atlassian config init` and the top-level `atlassian setup` / `atlassian configure` commands.
func runSetupWizard() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	name := flagProfile
	if name == "" {
		name = "default"
	}
	r := bufio.NewReader(os.Stdin)
	prof := cfg.Profiles[name]
	prof.URL = prompt.Ask(r, "Atlassian site URL (e.g. https://your-team.atlassian.net)", prof.URL)
	prof.Email = prompt.Ask(r, "Account email", prof.Email)
	fmt.Println("Create an API token at https://id.atlassian.com/manage-profile/security/api-tokens")
	tok, err := prompt.AskSecret(bufio.NewReader(os.Stdin), "API token")
	if err != nil {
		return err
	}
	if prof.Timeout == "" {
		prof.Timeout = "30s"
	}
	prof.VerifyTLS = prompt.AskBool(r, "Verify TLS certificates", true)

	cfg.Profiles[name] = prof
	if cfg.CurrentProfile == "" {
		cfg.CurrentProfile = name
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if tok != "" {
		if err := config.SetToken(name, prof.Email, tok); err != nil {
			return fmt.Errorf("store API token in keyring: %w", err)
		}
	}
	fmt.Printf("Saved profile %q to %s\n", name, cfg.Path())

	// Verify connectivity.
	svc, err := service.New(cfg, service.Overrides{Profile: name, Insecure: !prof.VerifyTLS})
	if err != nil {
		return err
	}
	c, cancel := ctx()
	defer cancel()
	u, err := svc.Client.Myself(c)
	if err != nil {
		fmt.Printf("Connection test FAILED: %v\n", err)
		return nil
	}
	fmt.Printf("Connected as %s (%s)\n", u.DisplayName, u.AccountID)
	return nil
}

var setupCmd = &cobra.Command{
	Use:     "setup",
	Aliases: []string{"configure"},
	Short:   "Interactive setup wizard (writes the config YAML)",
	Long:    "Prompts for the Atlassian site URL, account email and API token, writes the config YAML and stores the token in the OS keyring. Same as `atlassian config init`.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetupWizard()
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the active configuration (token masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		prof, name, err := cfg.Active(flagProfile)
		if err != nil {
			return err
		}
		hasTok := "no"
		if _, e := config.ResolveToken(name, prof.Email); e == nil {
			hasTok = "yes"
		}
		fmt.Printf("config file:     %s\n", cfg.Path())
		fmt.Printf("active profile:  %s\n", name)
		fmt.Printf("url:             %s\n", prof.URL)
		fmt.Printf("email:           %s\n", prof.Email)
		fmt.Printf("timeout:         %s\n", prof.Timeout)
		fmt.Printf("verify_tls:      %v\n", prof.VerifyTLS)
		fmt.Printf("token set:       %s\n", hasTok)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a profile key (url|email|timeout|verify_tls)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		name := cfg.ProfileName(flagProfile)
		prof := cfg.Profiles[name]
		key, val := strings.ToLower(args[0]), args[1]
		switch key {
		case "url":
			prof.URL = val
		case "email", "username":
			prof.Email = val
		case "timeout":
			prof.Timeout = val
		case "verify_tls":
			prof.VerifyTLS = val == "true" || val == "1" || val == "yes"
		default:
			return fmt.Errorf("unknown key %q", key)
		}
		cfg.Profiles[name] = prof
		if cfg.CurrentProfile == "" {
			cfg.CurrentProfile = name
		}
		return cfg.Save()
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a profile key value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		prof, _, err := cfg.Active(flagProfile)
		if err != nil {
			return err
		}
		switch strings.ToLower(args[0]) {
		case "url":
			fmt.Println(prof.URL)
		case "email", "username":
			fmt.Println(prof.Email)
		case "timeout":
			fmt.Println(prof.Timeout)
		case "verify_tls":
			fmt.Println(prof.VerifyTLS)
		default:
			return fmt.Errorf("unknown key %q", args[0])
		}
		return nil
	},
}

var configSetTokenCmd = &cobra.Command{
	Use:     "set-token",
	Aliases: []string{"set-password"},
	Short:   "Store the API token for the active profile in the OS keyring",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		prof, name, err := cfg.Active(flagProfile)
		if err != nil {
			return err
		}
		tok, err := prompt.AskSecret(bufio.NewReader(os.Stdin), "API token")
		if err != nil {
			return err
		}
		if err := config.SetToken(name, prof.Email, tok); err != nil {
			return err
		}
		fmt.Println("API token stored.")
		return nil
	},
}

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Verify connectivity and authentication (Jira + Confluence)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		u, err := svc.Client.Myself(c)
		if err != nil {
			return err
		}
		fmt.Printf("Jira OK - connected as %s (%s)\n", u.DisplayName, u.AccountID)
		if _, _, err := svc.Client.Spaces(c, nil, "", 1); err != nil {
			fmt.Printf("Confluence FAILED: %v\n", err)
		} else {
			fmt.Println("Confluence OK")
		}
		return nil
	},
}

var configProfilesCmd = &cobra.Command{
	Use:   "profiles",
	Short: "List configured profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		for name, p := range cfg.Profiles {
			marker := "  "
			if name == cfg.CurrentProfile {
				marker = "* "
			}
			fmt.Printf("%s%s\t%s\t%s\n", marker, name, p.Email, p.URL)
		}
		return nil
	},
}

var configUseCmd = &cobra.Command{
	Use:   "use <profile>",
	Short: "Switch the default profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if _, ok := cfg.Profiles[args[0]]; !ok {
			return fmt.Errorf("profile %q not found", args[0])
		}
		cfg.CurrentProfile = args[0]
		return cfg.Save()
	},
}

func init() {
	configCmd.AddCommand(configInitCmd, configShowCmd, configSetCmd, configGetCmd,
		configSetTokenCmd, configTestCmd, configProfilesCmd, configUseCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(setupCmd)
}
