-- The OSV provider's local mirror of advisory records. Keyed by provider
-- instance, so two OSV instances pointed at different mirrors stay
-- independent.
CREATE TABLE osv_advisories (
    provider_id UUID NOT NULL REFERENCES vulnerability_providers (id) ON DELETE CASCADE,
    id          TEXT NOT NULL,
    -- The OSV bucket directory (e.g. "Debian") the record was imported
    -- from, so a full re-import can replace exactly that directory's set.
    directory   TEXT NOT NULL,
    modified    TIMESTAMPTZ NOT NULL,
    doc         JSONB NOT NULL,
    PRIMARY KEY (provider_id, id)
);

CREATE INDEX osv_advisories_directory_idx ON osv_advisories (provider_id, directory);

-- Which (ecosystem, package) pairs each advisory affects - the lookup
-- matching uses, so it never scans advisory documents.
CREATE TABLE osv_affected_packages (
    provider_id  UUID NOT NULL,
    advisory_id  TEXT NOT NULL,
    ecosystem    TEXT NOT NULL,
    package_name TEXT NOT NULL,
    PRIMARY KEY (provider_id, advisory_id, ecosystem, package_name),
    FOREIGN KEY (provider_id, advisory_id) REFERENCES osv_advisories (provider_id, id) ON DELETE CASCADE
);

CREATE INDEX osv_affected_packages_lookup_idx ON osv_affected_packages (provider_id, ecosystem, package_name);

-- Import progress per OSV bucket directory (e.g. "Debian"). releases is
-- the set of release ecosystems (e.g. "Debian:12") the last full import
-- kept, so a release newly present in the fleet triggers a re-import.
CREATE TABLE osv_ecosystem_state (
    provider_id     UUID NOT NULL REFERENCES vulnerability_providers (id) ON DELETE CASCADE,
    directory       TEXT NOT NULL,
    releases        TEXT[] NOT NULL,
    newest_modified TIMESTAMPTZ,
    imported_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_id, directory)
);
