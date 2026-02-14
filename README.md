# OVH Dynamic DNS

A dynamic DNS updater for OVH-managed domains.

## Features

- Updates A records via the OVH API when your public IP changes
- IP caching — skips API calls entirely when the IP hasn't changed
- Multiple domains and subdomains in a single config
- systemd service with sandboxing (or cron)

## OVH API Credentials

1. Go to [OVH API Token Creation](https://api.ovh.com/createToken/)
2. Fill in the form:
   - **Application name**: Dynamic DNS Script
   - **Application description**: Script for updating DNS records
   - **Validity**: Unlimited (or set expiration)
   - **Rights**:
     - `GET /domain/zone/*`
     - `POST /domain/zone/*`
     - `PUT /domain/zone/*`
     - `DELETE /domain/zone/*`

3. Save the generated credentials:
   - Application Key
   - Application Secret
   - Consumer Key

## Build & Install

Requires Go 1.22+.

```bash
make build
sudo make install
```

This builds the binary, installs it to `/usr/local/bin/`, copies `config.json.example` to `/etc/ovh-dynamic-dns/config.json` (if no config exists yet), and installs the systemd unit files.

## Configuration

Edit `/etc/ovh-dynamic-dns/config.json` with your OVH credentials and domains. See [`config.json.example`](config.json.example) for the full structure.

Key fields:
- `ovh.endpoint` — API endpoint (`ovh-eu`, `ovh-us`, `ovh-ca`, `ovh-au`)
- `ovh.application_key`, `ovh.application_secret`, `ovh.consumer_key` — API credentials
- `domains[].zone` — the DNS zone (e.g. `example.com`)
- `domains[].subdomain` — subdomain to update (empty string for apex)
- `domains[].ttl` — TTL in seconds (defaults to 300)

Lock down permissions so only root can read the credentials:

```bash
sudo chown -R root:root /etc/ovh-dynamic-dns
sudo chmod 600 /etc/ovh-dynamic-dns/config.json
```

## Usage

Run manually to verify everything works:

```bash
sudo /usr/local/bin/ovh-dynamic-dns --config /etc/ovh-dynamic-dns/config.json
```

## Scheduling

### systemd timer (recommended)

The repository includes `ovh-dynamic-dns.service` and `ovh-dynamic-dns.timer`, which are installed to `/etc/systemd/system/` by `make install`.

Enable and start:

```bash
sudo systemctl enable --now ovh-dynamic-dns.timer
```

Check status:

```bash
sudo systemctl status ovh-dynamic-dns.timer
journalctl -u ovh-dynamic-dns.service
```

Check sandboxing score:

```bash
systemd-analyze security ovh-dynamic-dns.service
```

### cron

```bash
# Update DNS every 5 minutes
*/5 * * * * root /usr/local/bin/ovh-dynamic-dns --config /etc/ovh-dynamic-dns/config.json
```

## IP Caching

The program caches the current IP to avoid unnecessary OVH API calls. When the IP hasn't changed, the program exits immediately after a single HTTP request (to detect the public IP) and makes zero OVH API calls.

- **systemd**: cache is stored at `/var/lib/ovh-dynamic-dns/last_ip` (via `StateDirectory=`)
- **Other**: cache is stored as `.last_ip` next to the binary

To force a re-check (e.g., after manually changing DNS records), delete the cache file:

```bash
# systemd
sudo rm /var/lib/ovh-dynamic-dns/last_ip

# Other
sudo rm /usr/local/bin/.last_ip
```

## OVH Endpoints

- `ovh-eu`: Europe
- `ovh-us`: United States
- `ovh-ca`: Canada
- `ovh-au`: Australia

## License

This project is licensed under the [GPL-3.0 License](LICENSE).
