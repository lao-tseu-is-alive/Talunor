# Connecting the container to Ollama

Talunor bundles the embedding model, so **memory works offline** — only the
**chat** LLM (a local [Ollama](https://ollama.com)) is external. Reaching it from
the container needs one small piece of host setup, because two things are true by
default:

1. **Ollama listens on `127.0.0.1:11434` only.** It refuses any connection that
   isn't from loopback — and a container never is. *Keep it this way;* opening it
   up is the thing we want to avoid.
2. **Under Rancher Desktop / Docker Desktop the container runs in a VM.** So
   `--network host` gives you the *VM's* network, and `localhost:11434` inside the
   container finds nothing (`connection refused`). We reach the host by name
   instead: `--add-host=host.docker.internal:host-gateway`.

   > On **native Docker Engine on Linux** there is no VM — the container shares the
   > real host network, so `--network host` + `localhost:11434` just works and you
   > can skip everything below.

## The run command (all approaches)

```bash
nerdctl run --rm -it \
  --add-host=host.docker.internal:host-gateway \
  -e TALUNOR_OLLAMA_URL=http://host.docker.internal:<PORT>/v1 \
  -v talunor-data:/data \
  ghcr.io/lao-tseu-is-alive/talunor:latest
```

`docker run …` is identical. `<PORT>` is **11435** for the secure bridge
(A or B) or **11434** for the quick option. `--add-host=…:host-gateway` is the
portable way to resolve `host.docker.internal` (works on Rancher Desktop, Docker
Desktop, and native Docker).

---

## Recommended: keep Ollama on localhost, bridge only the VM

Ollama stays bound to `127.0.0.1`. You expose a **separate** port (`11435`) that
forwards to it, and a default-drop firewall lets **only the VM subnet** reach that
port. Nothing is reachable from your LAN.

The firewall is the security control. With nftables, in your default-drop `input`
chain (adjust the subnet to your VM's — Rancher Desktop's default is
`192.168.5.0/24`; confirm with `getent hosts host.docker.internal` inside a
container):

```nft
# table inet filter, chain input { type filter hook input priority 0; policy drop; ... }
tcp dport 11435 ip saddr 192.168.5.0/24 accept comment "Talunor bridge (VM only)"
```

Then pick one bridge:

### Option A — socat bridge (simple, explicit) — *recommended*

A tiny forwarder from the VM-reachable port to loopback Ollama, as a systemd unit
so it survives reboots:

```ini
# /etc/systemd/system/talunor-ollama-bridge.service
[Unit]
Description=Talunor: bridge the container VM to local Ollama
After=network-online.target ollama.service
Wants=network-online.target

[Service]
# Forward 11435 -> loopback Ollama. The default-drop firewall (above) is what
# keeps non-VM hosts out; binding 0.0.0.0 is fine because of it. If your host has
# a dedicated VM-facing address you may bind to it for extra defence in depth.
ExecStart=/usr/bin/socat TCP-LISTEN:11435,reuseaddr,fork,bind=0.0.0.0 TCP:127.0.0.1:11434
Restart=on-failure
DynamicUser=yes

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now talunor-ollama-bridge
```

Run the container with `<PORT>=11435`. Ollama never leaves `127.0.0.1`; only the
VM can reach the bridge.

### Option B — pure nftables (no helper daemon)

Redirect the VM's traffic straight to loopback Ollama with a DNAT — no socat.

> **Gotcha:** a DNAT (or `redirect`) whose target is a `127.0.0.0/8` address is
> **silently dropped** unless `route_localnet` is enabled on the incoming
> interface. If you write such a rule with `route_localnet=0`, it does *nothing*
> and you'll think the rule is wrong when it's really the sysctl.

```bash
# Persist in /etc/sysctl.d/99-talunor.conf, then `sysctl --system`.
# Replace <vm-iface> with the host interface facing the VM.
net.ipv4.conf.<vm-iface>.route_localnet = 1
```

```nft
# table ip nat, chain prerouting { type nat hook prerouting priority dstnat; policy accept; }
tcp dport 11435 ip saddr 192.168.5.0/24 dnat to 127.0.0.1:11434 comment "Talunor -> Ollama (VM only)"
```

DNAT rewrites the port to `11434` before the `input` chain, so the firewall rule
above should allow **`dport 11434`** (post-DNAT) rather than `11435`. Run the
container with `<PORT>=11435`. No extra process, but `route_localnet` is a sharper
tool: it is only safe because the default-drop policy + scoped `saddr` keep
everyone else off loopback.

---

## Quick, but exposes Ollama to your LAN

Simplest, and fine on a **trusted** network only — anything on your LAN can then
reach the model:

```bash
sudo systemctl edit ollama      # add:  [Service]\n  Environment="OLLAMA_HOST=0.0.0.0:11434"
sudo systemctl restart ollama
ss -tlnp | grep 11434           # should show 0.0.0.0:11434
```

Run the container with `<PORT>=11434`. No bridge or firewall rule needed — which
is exactly why it's less safe.

---

## Verifying

From inside a throwaway container you can confirm the path before running Talunor:

```bash
nerdctl run --rm --add-host=host.docker.internal:host-gateway alpine \
  sh -c 'nc -z -w3 host.docker.internal <PORT> && echo reachable || echo blocked'
```

`reachable` means Talunor will connect; `blocked` means the firewall rule, the
bridge, or the port is off.
