# Third-party node checks

`POST /api/v1/external-outbounds/{id}/check` and MCP `external_outbounds.check`
execute on the Controller host. No Agent, deployed path or `server_id` is required.
The Controller renders one outbound and invokes its bundled signed-release kernel
resource `downloads/oboard-sb-<os>-<arch> -check-outbound`. Controller never imports
or links sing-box. Linux amd64/arm64 packages and the updater preserve executable
permissions on those two resources; no additional service or listener is installed.

The command reads at most 64 KiB of JSON from stdin (credentials never appear in
argv or temporary configuration files), writes only `{"exit_ip":"8.8.8.8"}` on
success and exits nonzero with a credential-free error on failure. It validates
protocols and public proxy DNS answers, pins resolved addresses, rejects detours,
interface bindings and file references, and runs one isolated outbound without
inbound listeners or live configuration changes. Only fixed HTTPS IP sources
(`api.ip.sb/ip`, `icanhazip.com`) are contacted, with no redirects or environment
proxies. The kernel budget is ten seconds; Controller kills and reaps the process
after fifteen seconds and bounds stdout to 1 KiB. Controller serializes checks.

Controller validates the public exit IP again, resolves its country using GeoIP,
and stores credential-free results in `external_outbound_probe_results` with the
node connection fingerprint (`task_id=0` identifies Controller execution). A failed
recheck clears prior success; changed connection fields invalidate it. Deletion
cascades the result deletion. Manual region overrides remain authoritative.
Existing and newly created paths reuse the node result; deployment does not probe
each path. Explicit deployed-path `/probe-egress` diagnostics remain separate.

The Agent's signed `probe_external_egress` standalone form and Unix-only kernel
`/outbounds/check` endpoint retain their bounded validation contract; the node
management check no longer dispatches that Agent task.
