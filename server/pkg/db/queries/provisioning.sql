
-- name: ListProvisioningPins :many
SELECT * FROM provisioning_pin
WHERE workspace_id = $1
ORDER BY package_type, package_name;

-- name: ListEnabledProvisioningPins :many
SELECT * FROM provisioning_pin
WHERE workspace_id = $1 AND enabled = true
ORDER BY package_type, package_name;

-- name: DeleteProvisioningPinsForWorkspace :exec
DELETE FROM provisioning_pin
WHERE workspace_id = $1;

-- name: UpsertProvisioningPin :one
INSERT INTO provisioning_pin (
    workspace_id,
    package_name,
    package_type,
    version,
    enabled,
    updated_by
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, package_name, package_type)
DO UPDATE SET
    version    = EXCLUDED.version,
    enabled    = EXCLUDED.enabled,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by
RETURNING *;

-- name: UpsertDeliveredProvisioningPackages :exec
INSERT INTO provisioning_delivered_package (
    workspace_id,
    package_name,
    package_type,
    version
)
SELECT $1, unnest(@package_names::text[]), unnest(@package_types::text[]), unnest(@versions::text[])
ON CONFLICT (workspace_id, package_name, package_type, version)
DO UPDATE SET
    last_delivered_at = now();

-- name: ListDeliveredProvisioningPackages :many
SELECT * FROM provisioning_delivered_package
WHERE workspace_id = $1
ORDER BY package_type, package_name, version;
