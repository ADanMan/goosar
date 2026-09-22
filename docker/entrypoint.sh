#!/bin/sh
set -e

# The image runs as the unprivileged "goosar" user (uid 1001, #384). A stand
# created before that change has a backend_uploads volume (or a PVC) that Docker
# populated while the container was root, so its files are root-owned and the
# new uid cannot write them. Warn with the exact fix instead of failing on the
# first attachment upload. Deployments on S3 never touch this directory.
UPLOAD_DIR="${LOCAL_UPLOAD_DIR:-/app/data/uploads}"
if [ -d "$UPLOAD_DIR" ] && [ ! -w "$UPLOAD_DIR" ]; then
  echo "WARNING: $UPLOAD_DIR is not writable by uid $(id -u)."
  echo "         Local attachment uploads will fail. Fix the ownership once:"
  echo "         docker compose -f docker-compose.selfhost.yml run --rm --no-deps --user root --cap-add CHOWN \\"
  echo "           --entrypoint sh backend -c 'chown -R 1001:1001 /app/data/uploads'"
fi

echo "Running database migrations..."
./migrate up

echo "Starting server..."
exec ./server
