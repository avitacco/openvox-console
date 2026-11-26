## Put TLS in front of the console

The console serves its web interface and API over plain HTTP. The
`https://` base URL is reached through a reverse proxy that terminates
TLS and passes requests to the console. Any reverse proxy works. This
uses [Caddy](https://caddyserver.com/), on the console's host:

```sh tab="Debian and Ubuntu"
sudo apt-get install -y caddy
```

```sh tab="RHEL, Rocky and AlmaLinux"
sudo dnf install -y epel-release
sudo dnf install -y caddy
sudo systemctl enable --now caddy
```

This is then the whole `/etc/caddy/Caddyfile`:

```text
console.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

```sh
sudo systemctl reload caddy
```

If the console listens on another port, because it shares a host with
openvoxdb, put that port in `reverse_proxy` instead.

For a name the internet can resolve, Caddy obtains and renews the
certificate itself. For an internal name, give it a certificate from your
own CA with a `tls <cert file> <key file>` line inside the block. For a
trial, `tls internal` makes Caddy issue one from a local CA of its own,
which browsers warn about.

> [!WARNING]
> Only the web interface goes through the proxy. The node transport, port
> 8142, carries its own TLS and checks each node's certificate itself, so
> nodes must reach it directly. Never put it behind the proxy.
