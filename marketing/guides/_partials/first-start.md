## Log in for the first time

On its first start the console creates its database schema, then creates
the bootstrap admin while the users table is still empty. Its log shows
three lines once it is up:

```text
database migrations up to date
node transport ready   addr=[::]:8142
console ready          addr=:8080
```

Check that it reports itself healthy, first directly on the console's
host, then through the TLS proxy at its base URL:

```sh
curl -fsS http://127.0.0.1:8080/health
curl -fsS https://console.example.com/health
```

Each reply lists each dependency as `ok`. If only the second fails, the
console is fine and the proxy is not. Use the console's own port in the
first if you moved it off 8080. Then open the console's base URL,
log in as the bootstrap admin, and change its password straight away.

The bootstrap admin's password must have at least 12 characters and
must not be the same as its username. If it does not, no admin is
created and nobody can log in. The log says so:

```text
CONSOLE_BOOTSTRAP_ADMIN_PASSWORD does not meet the account policy, so no administrator was created
```

Set a longer password and start the console again. See
[Passwords and lockout](access.html#passwords-and-lockout).

> [!NOTE]
> Once you have logged in and changed the password, remove the bootstrap
> admin settings from the console's configuration. They are only read
> while the users table is empty, but there is no reason to keep a
> password lying around.
