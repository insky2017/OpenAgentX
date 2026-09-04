# AGY Graft Deployment

`agy-graft` is the only Runtime entrypoint for AGY Workers. Install the
repository copy at `/usr/local/libexec/openagentx/agy-graft` and record its
SHA-256 in the release manifest. Do not silently edit the machine-local copy
under `/home/sky/tools/bin`.

Mode and endpoint are resolved independently. Mode precedence is:

1. explicit `AGY_GRAFT_SELECT_PROXY_MODE`;
2. mode inferred from the matching `AGY_GRAFT_HTTP_PROXY` or
   `AGY_GRAFT_SOCKS5` endpoint;
3. mode inferred from a standard proxy URL scheme;
4. `select_proxy_mode` or an endpoint in `AGY_GRAFT_CONFIG`;
5. `only_http_proxy`.

After the mode is selected, endpoint precedence is:

1. the mode-matching `AGY_GRAFT_HTTP_PROXY` or `AGY_GRAFT_SOCKS5`;
2. a mode-matching standard `all_proxy`/`http_proxy`/`https_proxy` value with
   a compatible scheme;
3. the mode-matching endpoint in `AGY_GRAFT_CONFIG`;
4. the existing default `127.0.0.1:7897`.

An explicit mode does not make config unconditionally win over a compatible
standard proxy environment. An incompatible or mode-mismatched endpoint is
ignored for that mode and the next source is considered.

When the config file is selected, its format is the native `mgraftcp` format:

```text
select_proxy_mode = only_socks5
socks5 = proxy.example.invalid:28080
socks5_username = <username>
socks5_password = <password>
```

The path must be a regular, non-symlink file with mode `0600`, readable by the
Worker user. The wrapper rejects missing files, directories, symlinks, and any
other permission mode. Prefer this config path for authenticated SOCKS5:
`mgraftcp` then reads credentials from the file without the wrapper adding them
to argv.

Do not place real credentials in an Agent YAML file, unit file, repository, or
diagnostic output. Credential-bearing `AGY_GRAFT_SOCKS5`, `all_proxy`, or
`ALL_PROXY` values are a compatibility boundary: the current `mgraftcp` CLI
requires the wrapper to pass their username/password flags in argv. Use the
mode-`0600` config path when argv credential exposure is unacceptable. An
invalid explicitly configured file, an unsupported mode, a missing endpoint
in the selected config mode, or an unavailable local default proxy is
fail-closed.

For production systemd instances, put host-specific absolute paths in the
optional `/etc/openagentx/workers/%i.env` file. The unit template intentionally
does not force a config file to exist and does not depend on `/home/sky`:

```text
AGY_GRAFT_REAL_BIN=/opt/openagentx/bin/agy
AGY_GRAFT_MGRAFTCP_BIN=/usr/local/bin/mgraftcp
AGY_GRAFT_CONFIG=/etc/openagentx/workers/quote-service.agy-graft.conf
AGY_GRAFT_IPV4_ONLY_FILE=/etc/openagentx/workers/quote-service.ipv4-only.txt
```

The `AGY_GRAFT_CONFIG` line is optional; omit it when the selected endpoint is
provided by the environment. Interactive local use may continue to rely on
the wrapper's existing `/home/sky` defaults.

Install and verify the wrapper before starting the Worker:

```bash
install -D -m 0755 deploy/agy/agy-graft /usr/local/libexec/openagentx/agy-graft
sha256sum /usr/local/libexec/openagentx/agy-graft
stat -c '%a %U:%G' /etc/openagentx/agy-graft.conf
```

Each Worker YAML used by this unit must set an absolute `binary` path to the
installed wrapper and an absolute `working_dir`. The unit template injects an
explicit non-interactive `PATH` and optional per-instance EnvironmentFile; it
must not source `~/.zshrc` or call `set_proxy_server`.
