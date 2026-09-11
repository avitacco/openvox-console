## 1. Schema: node groups

- [x] 1.1 Add a migration for node groups (name, environment, priority)
      and verify it applies cleanly against a fresh database and is a
      no-op on rerun, matching the existing migration pattern
- [x] 1.2 Add migration(s) for a group's classes and per-class parameters
      and verify a group can be stored and read back with its classes
      intact
- [x] 1.3 Add migration(s) for a group's top-scope parameters and verify
      they round-trip correctly
- [x] 1.4 Add migration(s) for a group's rule conditions (fact path,
      operator, value) and pinned node certnames, and verify both
      round-trip correctly
- [x] 1.5 Add a uniqueness constraint on group priority and verify
      inserting a second group with a duplicate priority is rejected

## 2. classifier: group CRUD

- [x] 2.1 Implement create/read/update/delete for node groups (name,
      classes, parameters, environment, rule, pinned nodes, priority) and
      verify each operation against a real Postgres instance (`make up`)
- [x] 2.2 Verify creating a group with a duplicate priority returns a
      clear validation error, not a raw database error
- [x] 2.3 Verify updating a group's classes is reflected on the next read

## 3. classifier: matching and merge resolution

- [x] 3.1 Implement fact-based rule matching (`fact.path <op> value`,
      AND-only, operators `= != ~ > < >= <=`) and verify each operator
      against representative fact values, including nested fact paths
- [x] 3.2 Implement explicit node pinning and verify a pinned node matches
      its group even when the rule would not match
- [x] 3.3 Implement classification merge across all matching groups,
      resolving conflicts by priority, and verify: a higher-priority
      group's conflicting value wins; non-conflicting classes from
      different groups are unioned; environment resolves to the
      highest-priority matching group's value or is omitted if none set
      one
- [x] 3.4 Verify classification of a node matched by zero groups returns
      an empty result, not an error

## 4. classifier: console UI

- [x] 4.1 Add the group list page and verify it renders real groups from
      Postgres when loaded in a browser
- [x] 4.2 Add the create-group form and verify submitting it creates a
      real group, visible on the next list load
- [x] 4.3 Add the edit-group view (classes, parameters, rule, pinned
      nodes, priority) and verify changes persist and are reflected in
      classification
- [x] 4.4 Add group deletion and verify a deleted group no longer affects
      classification of any node

## 5. enc-api: HTTP endpoint

- [x] 5.1 Add `GET /api/v1/enc/{certname}` returning a node's merged
      classification (classes, parameters, environment) as JSON and
      verify it against real groups created via the API
- [x] 5.2 Verify the endpoint returns an empty classification (not an
      error) for a node matched by no group

## 6. enc-api: exec-terminus bridge

- [x] 6.1 Create `cmd/enc-bridge`: given a certname argument, call the ENC
      endpoint and marshal the JSON response to the exec-terminus YAML
      format (classes as a hash, parameters, optional environment) using
      `gopkg.in/yaml.v3`, and verify the output against Puppet's
      documented ENC format for a group with parameterized classes
- [x] 6.2 Verify `enc-bridge` exits 0 on success and writes valid YAML to
      stdout
- [x] 6.3 Add `enc-bridge` to the build (Makefile target, Dockerfile if
      applicable) - Dockerfile builds only the console's own image;
      enc-bridge is mounted into the separate openvoxserver container
      (task 7.1), not baked into ours, so no Dockerfile change applies

## 7. Integration: wire openvox-server to this console

- [x] 7.1 Configure the `openvoxserver` service (`docker-compose.yml`,
      `openvox` profile) with `node_terminus = exec` and `external_nodes`
      pointing at a mounted `enc-bridge` binary, and document the exact
      steps (mirroring how the console client cert step is documented in
      README.md)
- [x] 7.2 Create a real node group via the console (e.g. a class that
      manages a file resource, matching a fact `openvox-testing-agent`'s
      real facts satisfy) and verify via `make openvox-test` that the
      resulting agent run's compiled catalog includes that class's
      resource (this phase's exit criteria)
