`{}` means classification works and that node matches no group yet.
Create a node group on the console's **Groups** page that pins that
certname, run the same command again, and its classes come back:

```yaml
classes:
    ntp:
        servers:
            - time.example.com
parameters:
    role: prodtest
environment: production
```

An error here is the failure to catch. `permission denied` means the
token is not readable by the user openvoxserver runs as, and a 401 means
the token is wrong or lacks `enc:read`.

> [!TIP]
> If a node's next Puppet run fails with `Could not find class <name>`,
> classification is working: the console told openvoxserver to include a
> class your Puppet code does not define yet.
