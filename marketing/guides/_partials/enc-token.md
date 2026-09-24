## Connect classification

openvoxserver asks the console which classes each node gets, through
Puppet's standard `exec` node terminus. A small program, `enc-bridge`,
makes that request on openvoxserver's behalf, and it needs a token to do
so.

### Issue enc-bridge a token

A service token carries a fixed set of permissions chosen when it is
created, with no role involved. Create one allowed only to read
classification:

1. In the console, open **Admin**, then **Service tokens**.
2. Under **New service token**, name it `enc-bridge` and give it only the
   `enc:read` permission.
3. Choose **Create service token**.

The token is shown **once**. Copy it now.

> [!TIP]
> The same through the API, with an access token from an admin login:
> `curl -X POST https://console.example.com/api/v1/service-tokens -H "Authorization: Bearer <admin access token>" -H 'Content-Type: application/json' -d '{"name":"enc-bridge","permissions":["enc:read"]}'`
