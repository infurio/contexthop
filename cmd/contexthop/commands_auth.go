package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/resolver"
)

func runAuth(args []string) error {
	name, adc, terminal, err := parseAuthArgs(args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	identityName := name
	identity, ok := cfg.Identities[identityName]
	if !ok {
		resolved, resolveErr := resolver.Destination(cfg, name)
		if resolveErr != nil || resolved.Identity == nil {
			return fmt.Errorf("unknown identity or workspace %q", name)
		}
		identityName = resolved.IdentityName
		identity = *resolved.Identity
	}
	return cloudauth.ExplicitLogin(context.Background(), identityName, identity, adc, terminal)
}

func parseAuthArgs(args []string) (name string, adc, terminal bool, err error) {
	options := true
	for _, arg := range args {
		switch {
		case options && arg == "--":
			options = false
		case options && arg == "--adc" && !adc:
			adc = true
		case options && arg == "--adc":
			return "", false, false, errors.New("--adc may only be specified once")
		case options && arg == "--terminal" && !terminal:
			terminal = true
		case options && arg == "--terminal":
			return "", false, false, errors.New("--terminal may only be specified once")
		case options && strings.HasPrefix(arg, "-"):
			return "", false, false, fmt.Errorf("unknown auth option %q", arg)
		case name == "":
			name = arg
		default:
			return "", false, false, errors.New("usage: chop auth [--adc] [--terminal] <identity-or-workspace>")
		}
	}
	if name == "" {
		return "", false, false, errors.New("usage: chop auth [--adc] [--terminal] <identity-or-workspace>")
	}
	return name, adc, terminal, nil
}
