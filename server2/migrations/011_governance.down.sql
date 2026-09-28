DROP TABLE IF EXISTS provisioning_pins;
DROP TABLE IF EXISTS platform_policy;
ALTER TABLE space_mcp_servers DROP CONSTRAINT IF EXISTS space_mcp_servers_platform_fk;
DROP TABLE IF EXISTS platform_mcp_servers;
DROP TABLE IF EXISTS platform_audit_log;
DROP TABLE IF EXISTS platform_admin_requests;
DROP TABLE IF EXISTS platform_admins;
