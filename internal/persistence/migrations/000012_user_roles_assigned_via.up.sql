ALTER TABLE user_roles ADD COLUMN assigned_via TEXT NOT NULL DEFAULT 'manual'
    CHECK (assigned_via IN ('manual', 'oidc'));
