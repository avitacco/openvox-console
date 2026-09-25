## Check the whole deployment

| Check | Expect |
| --- | --- |
| `curl -ksS https://<openvoxserver>:8140/status/v1/simple` | `running` |
| `curl -ksS https://<openvoxdb>:8081/status/v1/simple` | `running` |
| `curl -fsS https://console.example.com/health` | every dependency `ok` |
| The console's log on start | migrations, node transport, console ready |
| Logging in as the bootstrap admin | succeeds, and the password has been changed |
| The **Groups** page | a group matches the nodes you expect |

Your console is ready for nodes. Continue with
[Add nodes](add.html) to enroll the first one.
