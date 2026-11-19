ALTER TABLE jobs DROP COLUMN approved_by;
ALTER TABLE jobs DROP COLUMN agent_token;
ALTER TABLE jobs DROP COLUMN agent_request_id;
DROP TABLE agent_requests;
DROP TABLE agent_tokens;
