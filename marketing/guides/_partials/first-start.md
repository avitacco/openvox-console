## Log in for the first time

On its first start the console creates its database schema, then creates
the bootstrap admin while the users table is still empty. Its log shows
three lines once it is up:

```text
database migrations up to date
node transport ready   addr=[::]:8142
console ready          addr=:8080
```

Check that it reports itself healthy:

```sh
curl -fsS https://console.example.com/health
```

The reply lists each dependency as `ok`. Then open the console's base URL,
log in as the bootstrap admin, and change its password straight away.

> [!NOTE]
> Once you have logged in and changed the password, remove the bootstrap
> admin settings from the console's configuration. They are only read
> while the users table is empty, but there is no reason to keep a
> password lying around.
