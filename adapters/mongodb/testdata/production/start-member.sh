#!/bin/bash
set -euo pipefail

: "${RIDU_MONGODB_MEMBER_PORT:?member port is required}"

install -d -o mongodb -g mongodb -m 0700 /data/db /data/configdb
install -o mongodb -g mongodb -m 0400 /ridu-fixture/secrets/keyfile /data/configdb/keyfile
install -o mongodb -g mongodb -m 0400 /ridu-fixture/secrets/server.pem /data/configdb/server.pem
install -o mongodb -g mongodb -m 0444 /ridu-fixture/secrets/ca.pem /data/configdb/ca.pem
chown -R mongodb:mongodb /data/db

exec gosu mongodb mongod \
	--auth \
	--bind_ip 127.0.0.1 \
	--dbpath /data/db \
	--keyFile /data/configdb/keyfile \
	--nounixsocket \
	--oplogSize 128 \
	--port "${RIDU_MONGODB_MEMBER_PORT}" \
	--replSet ridu-production-rs0 \
	--tlsAllowConnectionsWithoutCertificates \
	--tlsCAFile /data/configdb/ca.pem \
	--tlsCertificateKeyFile /data/configdb/server.pem \
	--tlsMode requireTLS \
	--wiredTigerCacheSizeGB 0.25
