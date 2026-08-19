package main

import "fmt"

var plannedModules = map[string]string{
	"github":    "GitHub",
	"bitbucket": "Bitbucket",
	"asana":     "Asana",
}

func cmdPlanned(name string) error {
	return fmt.Errorf("the %s module is not implemented yet (reserved for: %s)", name, plannedModules[name])
}
