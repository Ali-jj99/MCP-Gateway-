ALTER TABLE audit_logs ADD COLUMN request_body TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN response_body TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN tool_name TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_audit_logs_tool_name ON audit_logs (tool_name) WHERE tool_name != '';
CREATE INDEX idx_audit_logs_status_code ON audit_logs (status_code);
