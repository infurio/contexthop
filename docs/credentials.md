# Credentials and isolation

[Documentation](README.md) · [ADC controls](usage.md#adc)

## Login and ADC

Gcloud CLI credentials and Application Default Credentials (ADC) are configured
separately, but do not necessarily require separate browser sign-ins:

```sh
chop auth <identity>
chop auth <identity> --adc
```

To prepare ADC without launching or changing a shell, highlight an identity and
choose **Authentication** (`a`, or through Options) → **ADC login**. CLI login is not
required. Chop verifies the ADC account and returns to Identities, retaining the
current selection and ADC toggle.

**Authentication → ADC logout** revokes the identity's configured user ADC and
removes its local file, including a custom ADC path and any matching default
copy. It affects every shell and application using those credentials. Credentials
shared with gcloud (for example, through combined login) may also stop working;
CLI credential files are not deleted. The identity's CLI status returns to
“Not checked” after revocation. To exclude ADC from just one shell, disable ADC
for that selection instead of logging out.

If revocation fails, local credentials are retained and Authentication shows the
error for retry. ADC logout supports Google user credentials; service-account
keys and other credential types are left unchanged. A missing selected ADC file
is treated as already logged out. Other identities' separately configured
credentials are not selected for revocation.

Following a published default copies its already verified snapshot locally. Prompt
and pre-command hooks never launch authentication or make provider requests;
provider tools handle credential refresh or expiry when used.

Preparing ADC does not enable it for every launch. Workspaces default to no ADC;
`adc: identity` opts a workspace in. The TUI's ADC override changes the prepared
context and changes a saved workspace only when explicitly saved.

When enabled, explicit launches and shared-config publication verify the selected
ADC file's account with Google before committing state or starting a command. This includes
session reuse and reselecting an equivalent active context. In the TUI, launching
a selection with ADC enabled checks both credential configurations. If both need
authentication, the login prepares both using `gcloud auth login --update-adc`.
Valid ADC is preserved when only CLI authentication is needed. If CLI login is
already valid, ADC preparation uses `gcloud auth application-default login` with
the selected account, allowing gcloud to reuse available credentials before
requesting another sign-in. Both paths use the identity's isolated configuration
and verify ADC before resuming the original shared-config, current-shell or subshell launch;
a saved workspace is not required. Cancelling or failing authentication leaves
the active shell context unchanged. Command-line launches and session reuse
retain an authentication recovery command when verification fails.
Verification itself is bounded to ten seconds and never opens a browser.
ADC-off launches skip that check and point `GOOGLE_APPLICATION_CREDENTIALS`
to a deliberately absent session-local file to prevent ambient fallback.

Browsing and previews read local catalog state without authenticating. Discovery
and authenticated launches check the selected identity's login. Interactive
login offers browser or terminal authentication and resumes the pending operation;
noninteractive commands fail with recovery instructions instead of opening a browser.

On macOS, authentication browser selection prefers an exact-account Chrome
profile, then a unique same-domain profile; explicit `BROWSER` wins.
This differs from [Console navigation](usage.md#google-cloud-console), which uses
a saved binding or a unique exact-account Chrome/Edge match and never a domain match.

## Session boundaries

**Update shared** updates all integrated terminals following the shared config.
Pinned shells (**Pin**) and subshells keep their selections. Following
terminals adopt changes at prompts and before commands; running applications
retain their startup environment.

Each session gets its own kubeconfig and applicable cloud/runtime bindings.
Unselected components use disabled bindings rather than inherited global state.
Docker selection does not run a global `docker context use`.
Explicitly sharing existing `cloudSdkConfig` or `adc` paths shares credential
state, so keep those paths separate for isolation.

Activation checks local sources and control-plane fingerprints, not project,
Kubernetes API, namespace, or Docker reachability. An uncached GKE target needs
`gcloud container clusters get-credentials` to materialize its configuration.
Provider tools report runtime access or connectivity failures.

Imported kubeconfigs retain their sources. Different credentials, endpoints, or
TLS definitions stay separately selectable; conflicting names require a specific
source. Namespace-only variants are aliases of one access profile. Native
namespace changes affect only the session copy and appear as drift in `chop status`.

Imported kubeconfigs are cached by source revision; generated GKE configurations
are cached for thirty minutes. Every activation receives an independent copy.
Close managed terminals and remove the optional startup hook when uninstalling;
the original tool configuration remains available.
