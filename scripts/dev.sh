#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_PORT="${DEV_BACKEND_PORT:-33299}"
FRONTEND_PORT="${DEV_FRONTEND_PORT:-5173}"
BACKEND_URL="http://127.0.0.1:${BACKEND_PORT}"
DEV_BIN="${ROOT}/dist/uiautodev-server-dev"
BACKEND_LOG="${TMPDIR:-/tmp}/uiautodev-dev-backend.log"
BACKEND_PID=""
WATCH_PID=""

cleanup() {
	if [[ -n "${WATCH_PID}" ]] && kill -0 "${WATCH_PID}" 2>/dev/null; then
		kill "${WATCH_PID}" 2>/dev/null || true
		wait "${WATCH_PID}" 2>/dev/null || true
	fi
	stop_backend
	rm -f "${DEV_BIN}.tmp"
}
trap cleanup EXIT INT TERM

stop_backend() {
	if [[ -n "${BACKEND_PID}" ]] && kill -0 "${BACKEND_PID}" 2>/dev/null; then
		kill "${BACKEND_PID}" 2>/dev/null || true
		wait "${BACKEND_PID}" 2>/dev/null || true
	fi
	BACKEND_PID=""
}

port_pids() {
	local port="$1"
	lsof -ti "tcp:${port}" -sTCP:LISTEN 2>/dev/null || true
}

# Free a port: stop whatever is already listening on it.
stop_port_occupant() {
	local port="$1" pids pids_display
	pids="$(port_pids "${port}" | tr '\n' ' ')"
	pids="${pids% }"
	[[ -z "${pids}" ]] && return 0

	pids_display="${pids// /, }"
	echo "Port ${port} is in use (pid: ${pids_display}); stopping it..."
	# shellcheck disable=SC2086
	kill ${pids} 2>/dev/null || true
	for _ in $(seq 1 30); do
		[[ -z "$(port_pids "${port}")" ]] && return 0
		sleep 0.1
	done

	echo "Port ${port} still in use; forcing stop." >&2
	# shellcheck disable=SC2086
	kill -9 ${pids} 2>/dev/null || true
	for _ in $(seq 1 20); do
		[[ -z "$(port_pids "${port}")" ]] && return 0
		sleep 0.1
	done

	echo "Failed to free port ${port}." >&2
	return 1
}

backend_hash() {
	case "$(uname -s)" in
	Darwin | FreeBSD)
		{
			find "${ROOT}/cmd" "${ROOT}/webserver" "${ROOT}/version" -type f -name '*.go' -exec stat -f '%m %z %N' {} +
			find "${ROOT}" -maxdepth 1 -type f -name '*.go' -exec stat -f '%m %z %N' {} +
			stat -f '%m %z %N' "${ROOT}/go.mod" "${ROOT}/go.sum"
		} | sort | cksum
		;;
	*)
		{
			find "${ROOT}/cmd" "${ROOT}/webserver" "${ROOT}/version" -type f -name '*.go' -exec stat -c '%Y %s %n' {} +
			find "${ROOT}" -maxdepth 1 -type f -name '*.go' -exec stat -c '%Y %s %n' {} +
			stat -c '%Y %s %n' "${ROOT}/go.mod" "${ROOT}/go.sum"
		} | sort | cksum
		;;
	esac
}

build_backend() {
	rm -f "${DEV_BIN}.tmp"
	if ! go build -trimpath -o "${DEV_BIN}.tmp" ./cmd/uiautodev-server; then
		rm -f "${DEV_BIN}.tmp"
		return 1
	fi
	mv "${DEV_BIN}.tmp" "${DEV_BIN}"
}

start_backend() {
	"${DEV_BIN}" -addr "127.0.0.1:${BACKEND_PORT}" -proxy-log "${ROOT}/proxy.log" -verbose >>"${BACKEND_LOG}" 2>&1 &
	BACKEND_PID=$!
	for _ in $(seq 1 50); do
		if curl -fsS "${BACKEND_URL}/healthz" >/dev/null 2>&1; then
			return 0
		fi
		if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
			echo "Backend failed to start; see ${BACKEND_LOG}" >&2
			return 1
		fi
		sleep 0.15
	done
	echo "Backend did not become healthy; see ${BACKEND_LOG}" >&2
	return 1
}

watch_backend() {
	local last current
	last="$(backend_hash)"
	while true; do
		sleep 1
		current="$(backend_hash)"
		[[ "${current}" == "${last}" ]] && continue
		last="${current}"
		echo "[dev] backend changed, rebuilding..."
		if ! build_backend; then
			echo "[dev] backend build failed; keeping the running process" >&2
			continue
		fi
		stop_backend
		if ! start_backend; then
			return 1
		fi
		echo "[dev] backend restarted"
	done
}

# Free both dev ports. Frontend first: killing a previous dev.sh instance lets
# its own trap stop the backend and file watcher before we start ours.
stop_port_occupant "${FRONTEND_PORT}"
stop_port_occupant "${BACKEND_PORT}"

# go:embed needs a populated frontend_build even in dev mode.
if [[ ! -e "${ROOT}/cmd/uiautodev-server/frontend_build" ]]; then
	echo "Preparing embedded frontend assets..."
	mkdir -p "${ROOT}/cmd/uiautodev-server/frontend_build"
	echo hello >"${ROOT}/cmd/uiautodev-server/frontend_build/hello.txt"
fi

cd "${ROOT}/frontend"
if [[ ! -d node_modules ]]; then
	echo "Installing frontend dependencies..."
	bun install
fi

cd "${ROOT}"
mkdir -p dist
echo "Building backend..."
build_backend

echo ""
echo "uiautodev dev"
echo "  Backend:  ${BACKEND_URL}  (watching Go files)"
echo "  Frontend: http://localhost:${FRONTEND_PORT}"
echo "  Log:      ${BACKEND_LOG}"
echo ""

start_backend
watch_backend &
WATCH_PID=$!

cd "${ROOT}/frontend"
exec bun run dev --port "${FRONTEND_PORT}" --strictPort
