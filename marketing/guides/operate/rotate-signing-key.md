---
title: Rotate the token signing key
summary: Replace the key the console signs login tokens with, without logging anyone out.
order: 2
---

The console signs its login tokens with its own ES256 key. Each token
names the key that signed it, and the console checks tokens against a
**set** of keys. So a new key can take over signing while tokens signed
by the old one keep working until they expire: nobody is logged out, and
nothing stops.

Three settings are involved:

| Setting | What it is |
| --- | --- |
| `CONSOLE_RBAC_SIGNING_KEY_FILE` | the key this instance signs with |
| `CONSOLE_RBAC_SIGNING_KEY_ID` | the key's ID, written into every token it signs. Unset, it is `default` |
| `CONSOLE_RBAC_VERIFICATION_KEYS_DIR` | a directory of further keys to accept tokens from, one file per key, named `<key ID>.pem` |

An instance always accepts tokens signed with its own signing key.

## Generate the new key

Choose an ID for it; a date is a good one:

```sh
openssl ecparam -genkey -name prime256v1 -noout -out 2026-10-01.pem
```

## Make every instance accept both keys

On **every** console instance, put the current key and the new one in
its verification directory, each named after its ID. A deployment that
never set `CONSOLE_RBAC_SIGNING_KEY_ID` uses `default` for the current
key:

```sh
sudo install -d -m 0750 -o root -g openvox-console /etc/openvox-console/rbac-verification-keys
sudo install -m 0640 -o root -g openvox-console \
  /etc/openvox-console/rbac-signing-key.pem /etc/openvox-console/rbac-verification-keys/default.pem
sudo install -m 0640 -o root -g openvox-console \
  2026-10-01.pem /etc/openvox-console/rbac-verification-keys/2026-10-01.pem
```

Set `CONSOLE_RBAC_VERIFICATION_KEYS_DIR` to that directory if it is not
set already, and restart each instance.

Doing this everywhere **before** anything signs with the new key is what
keeps a load-balanced deployment working: a token one instance signs with
the new key is then accepted by every other.

## Start signing with the new key

On the instances that issue tokens (`all` and `web`), sign with the new
key under its ID, and restart them:

```ini
CONSOLE_RBAC_SIGNING_KEY_FILE=/etc/openvox-console/rbac-verification-keys/2026-10-01.pem
CONSOLE_RBAC_SIGNING_KEY_ID=2026-10-01
```

New logins now get tokens signed with the new key. Tokens signed with the
old one keep working, because every instance still accepts it.

## Retire the old key

A token lives at most 24 hours: that is how long a refresh token lasts.
Once 24 hours have passed since the last instance started signing with
the new key, remove the old key's file from every instance's
verification directory, and restart them. Tokens signed with the old key
are refused from then on.

> [!CAUTION]
> Removing the old key sooner logs out everyone whose tokens it signed.
> Removing it and then losing the new key logs out everyone. Back the new
> key up before retiring the old one.
