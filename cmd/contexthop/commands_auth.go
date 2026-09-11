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
	name, adc, err := parseAuthArgs(args)
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
	if adc {
		return cloudauth.LoginADC(context.Background(), identityName, identity)
	}
	return cloudauth.Login(context.Background(), identityName, identity)
}

func parseAuthArgs(args []string) (name string, adc bool, err error) {
	for _, arg := range args {
		switch {
		case arg == "--adc" && !adc:
			adc = true
		case arg == "--adc":
			return "", false, errors.New("--adc may only be specified once")
		case strings.HasPrefix(arg, "-"):
			return "", false, fmt.Errorf("unknown auth option %q", arg)
		case name == "":
			name = arg
		default:
			return "", false, errors.New("usage: chop auth [--adc] <identity-or-workspace>")
		}
	}
	if name == "" {
		return "", false, errors.New("usage: chop auth [--adc] <identity-or-workspace>")
	}
	return name, adc, nil
}
