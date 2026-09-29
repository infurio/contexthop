package main

import (
	"context"
	"fmt"
	"strings"

	cloudauth "github.com/infurio/contexthop/internal/auth"
)

// authenticationRequired means Chop stopped before starting the requested child.
type authenticationRequired struct {
	identity string
	adc      bool
}

func (e *authenticationRequired) Error() string {
	command := "chop auth "
	if e.adc {
		command = "chop auth --adc "
	}
	if strings.HasPrefix(e.identity, "-") {
		command += "-- "
	}
	command += quoteShellArgument(e.identity)
	return "authentication required; command not started. Run in your own terminal: " + command + "; then retry"
}

type loginDisabledKey struct{}

// withLoginDisabled leaves the child terminal interactive but hands any login
// back to the user. The option belongs to this launch, not its environment.
func withLoginDisabled(ctx context.Context) context.Context {
	return context.WithValue(ctx, loginDisabledKey{}, true)
}

func loginDisabled(ctx context.Context) bool {
	disabled, _ := ctx.Value(loginDisabledKey{}).(bool)
	return disabled
}

// Call only for credential checks performed before the child is started. A
// failed child command must never receive this automatic-retry guidance.
func launchAuthError(err error) error {
	if cloudauth.IsProviderAccessDenied(err) {
		return fmt.Errorf("provider_access_denied; command not started. Local credential access was denied. If running in an agent sandbox, retry this same command once with host approval; if that fails too, report the access error: %w", err)
	}
	return err
}

func quoteShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
