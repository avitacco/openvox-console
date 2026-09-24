### The token signing key

The console signs its own login tokens with an ES256 key. This one does
not come from the CA. Generate it on any machine with OpenSSL:

```sh
openssl ecparam -genkey -name prime256v1 -noout -out rbac-signing-key.pem
```

> [!CAUTION]
> Back this file up. Losing it invalidates every issued token, which logs
> everyone out; leaking it lets anyone mint tokens. See
> [Rotate the token signing key](rotate-signing-key.html) to replace it
> safely.
