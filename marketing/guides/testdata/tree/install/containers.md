---
title: Install with containers
summary: From an empty host to a working console, with Docker Compose.
order: 2
---

## Start the CA

Run this first. It's the CA, so everything else depends on it.

```sh
docker compose up -d openvoxserver
printf '%s' '<password>' > "secrets/a & b"

echo after a blank line
```

> [!WARNING]
> Don't sign `<certname>` requests you did not expect.

- Tight item with `code`
- Item with a nested list
  - Nested item

1. Loose item.

   Second paragraph of it.

| Setting | Meaning |
| --- | --- |
| `CONSOLE_HTTP_ADDR` | Where the console listens |

<!-- include: verify.md -->

## Start the CA

A duplicate heading gets a suffixed anchor.

![](shot:dashboard)
