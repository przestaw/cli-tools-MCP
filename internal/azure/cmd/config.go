// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"workctl/internal/azure/config"
	"workctl/internal/azure/service"
	"workctl/internal/prompt"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage azure configuration (profiles, credentials)",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup wizard for a profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetupWizard()
	},
}

// runSetupWizard prompts for connection details, writes the config YAML,
// stores the PAT in the keyring, then verifies connectivity. Shared by
// `azure config init` and the top-level `azure setup` / `azure configure` commands.
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
	prof.OrgURL = normalizeOrgURL(prompt.Ask(r, "Organization URL or name (e.g. https://dev.azure.com/your-org)", prof.OrgURL))
	prof.Project = prompt.Ask(r, "Default team project (optional)", prof.Project)
	fmt.Println("Create a PAT at https://dev.azure.com/<org>/_usersSettings/tokens (scopes: Code, Build, Release as needed)")
	tok, err := prompt.AskSecret(bufio.NewReader(os.Stdin), "Personal Access Token")
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
		if err := config.SetToken(name, prof.OrgURL, tok); err != nil {
			return fmt.Errorf("store PAT in keyring: %w", err)
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
	cd, err := svc.Client.ConnectionData(c)
	if err != nil {
		fmt.Printf("Connection test FAILED: %v\n", err)
		return nil
	}
	fmt.Printf("Connected as %s\n", cd.AuthenticatedUser.ProviderDisplayName)
	return nil
}

// normalizeOrgURL accepts a bare org name or a full URL and returns a full URL.
func normalizeOrgURL(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	return "https://dev.azure.com/" + s
}

var setupCmd = &cobra.Command{
	Use:     "setup",
	Aliases: []string{"configure"},
	Short:   "Interactive setup wizard (writes the config YAML)",
	Long:    "Prompts for the organization URL, default project and PAT, writes the config YAML and stores the token in the OS keyring. Same as `azure config init`.",
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
		if _, e := config.ResolveToken(name, prof.OrgURL); e == nil {
			hasTok = "yes"
		}
		fmt.Printf("config file:     %s\n", cfg.Path())
		fmt.Printf("active profile:  %s\n", name)
		fmt.Printf("org_url:         %s\n", prof.OrgURL)
		fmt.Printf("project:         %s\n", prof.Project)
		fmt.Printf("timeout:         %s\n", prof.Timeout)
		fmt.Printf("verify_tls:      %v\n", prof.VerifyTLS)
		fmt.Printf("token set:       %s\n", hasTok)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a profile key (org_url|project|timeout|verify_tls)",
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
		case "org_url", "org", "organization":
			prof.OrgURL = normalizeOrgURL(val)
		case "project":
			prof.Project = val
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
		case "org_url", "org", "organization":
			fmt.Println(prof.OrgURL)
		case "project":
			fmt.Println(prof.Project)
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
	Aliases: []string{"set-pat", "set-password"},
	Short:   "Store the PAT for the active profile in the OS keyring",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		prof, name, err := cfg.Active(flagProfile)
		if err != nil {
			return err
		}
		tok, err := prompt.AskSecret(bufio.NewReader(os.Stdin), "Personal Access Token")
		if err != nil {
			return err
		}
		if err := config.SetToken(name, prof.OrgURL, tok); err != nil {
			return err
		}
		fmt.Println("PAT stored.")
		return nil
	},
}

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Verify connectivity and authentication",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		cd, err := svc.Client.ConnectionData(c)
		if err != nil {
			return err
		}
		fmt.Printf("Auth OK - connected as %s\n", cd.AuthenticatedUser.ProviderDisplayName)
		ps, err := svc.Client.Projects(c, false)
		if err != nil {
			fmt.Printf("Projects FAILED: %v\n", err)
			return nil
		}
		fmt.Printf("Org OK - %d project(s) visible\n", len(ps))
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
			fmt.Printf("%s%s\t%s\t%s\n", marker, name, p.Project, p.OrgURL)
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
