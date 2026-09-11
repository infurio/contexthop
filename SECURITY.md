# Security policy

## Reporting a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/infurio/contexthop/security/advisories/new)
for suspected credential exposure, context isolation failures, unsafe file
handling, or other vulnerabilities. Do not disclose these through public issues
or pull requests.

Include the affected version, a minimal reproduction with fictional resources,
expected and actual behaviour, and the potential impact. Do not send working
credentials, tokens, or private kubeconfigs, even in a private report.

If a credential has already been exposed, revoke or rotate it with its provider.
Removing a public message does not invalidate a credential.

## Versions and scope

Please check the latest stable release when practical and identify the version
you tested. Security fixes target the latest release; older versions may need
an upgrade. There is no guaranteed response or resolution timeframe.

Read [credentials and isolation](docs/credentials.md) for the supported security
boundaries, including explicitly shared credential paths. Provider authentication
and authorization remain the responsibility of the provider tools and services.
