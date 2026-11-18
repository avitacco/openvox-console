-- Pending-update snapshots, one per node (add-patch-inventory).
--
-- node_patch_scans holds the latest scan of each node and node_patch_updates
-- its updates. A scan replaces both in one transaction, so a reader never
-- sees half of one. scanned_at is the last successful scan and attempted_at
-- the last try: a failed attempt sets attempted_at and error and leaves the
-- last good updates in place.
--
-- certname is not a foreign key: nodes live in openvoxdb and the CA, not here.
CREATE TABLE node_patch_scans (
    certname        TEXT PRIMARY KEY,
    manager         TEXT NOT NULL DEFAULT '',
    reboot_required BOOLEAN NOT NULL DEFAULT FALSE,
    update_count    INT NOT NULL DEFAULT 0,
    security_count  INT NOT NULL DEFAULT 0,
    scanned_at      TIMESTAMPTZ,
    attempted_at    TIMESTAMPTZ NOT NULL,
    error           TEXT NOT NULL DEFAULT '',
    -- The node has no package manager the scan knows, or an agent that
    -- predates it.
    unsupported     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE node_patch_updates (
    certname  TEXT NOT NULL REFERENCES node_patch_scans (certname) ON DELETE CASCADE,
    name      TEXT NOT NULL,
    installed TEXT NOT NULL DEFAULT '',
    available TEXT NOT NULL,
    security  BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (certname, name)
);

CREATE INDEX node_patch_updates_name ON node_patch_updates (name);

-- A single row: the periodic scan's interval and whether it runs.
CREATE TABLE patch_scan_settings (
    id               BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    interval_minutes INT NOT NULL DEFAULT 1440 CHECK (interval_minutes BETWEEN 15 AND 10080)
);

INSERT INTO patch_scan_settings DEFAULT VALUES;
