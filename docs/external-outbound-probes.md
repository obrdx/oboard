# Third-party node checks

### Independent third-party proxy availability probe

`probe_external_egress` accepts an independent node check with `standalone`:

```json
{"version":0,"timeout_ms":10000,"targets":null,"standalone":{"external_outbound_id":2,"fingerprint":"64-character-sha256","outbound":{"type":"http","tag":"external-probe","server":"proxy.example.com","server_port":8080,"username":"example","password":"example"}}}
```

Controller renders the outbound; Agent does not invent configuration. `standalone`
and path `targets` are mutually exclusive. No applied configuration version is
required. Agent advertises `external_outbound_check_v1`, kernel advertises
`external_outbound_probe_v1`; Controller requires both. Installer/update layouts are
unchanged, but Agent and kernel must both be updated before this operation is used.

Agent POSTs the bounded outbound JSON to the Unix-only `/outbounds/check` API with a
12-second deadline. Kernel allows one probe at a time, limits input to 64 KiB and
execution to ten seconds, rejects direct/detour/interface/file-backed options,
validates every DNS answer as public, and pins the proxy address before dialing.
It starts and closes an isolated outbound instance with no inbound listeners or
changes to the live instance. Only fixed HTTPS IP sources (`api.ip.sb/ip`,
`icanhazip.com`) are contacted through that outbound; redirects/environment proxies
are disabled, TLS uses the runtime clock, response bodies are bounded, and only
public exit IPs are accepted. Errors never echo credentials.

The signed result uses the existing `items` array with one item: `probe_id` equals
the configuration fingerprint, `status` is succeeded/failed, and `exit_ip` contains
the observed address on success. Controller binds the callback to the issued task,
node fingerprint and latest task ID, independently validates the exit IP, performs
GeoIP lookup and stores the result in `external_outbound_probe_results`. A failed
recheck clears the prior success. A changed connection fingerprint invalidates the
result; stale callbacks cannot overwrite a newer attempt. No credentials are stored
in the probe-result table. Node deletion cascades the result deletion.

Automatic path deployment no longer includes an egress probe. New and existing
paths inherit the independently measured node region, subject to explicit manual
region overrides. The explicit deployed-path `/probe-egress` command remains
available for path-specific diagnostics.
