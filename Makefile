# Builds a single deployable Nook binary: the Go backend with the
# frontend build embedded inside it (internal/webui). See
# docs/ARCHITECTURE.md for the full shape.
#
# Usage:
#   make build     # frontend build + embed + go build -> backend/nook
#   make frontend  # just build the frontend (frontend/dist)
#   make embed     # frontend + copy frontend/dist into the Go embed dir
#   make clean     # remove all build output
#
# Day-to-day development doesn't need any of this -- run the backend and
# frontend dev servers separately (see docs/ARCHITECTURE.md, "Running
# locally"), which gives instant reload on both sides. This Makefile is
# for producing the final, single-binary artifact.

.PHONY: build frontend embed clean

build: embed
	cd backend && go build -o nook ./cmd/nook

frontend:
	cd frontend && npm ci && npm run build

embed: frontend
	rm -rf backend/internal/webui/dist
	mkdir -p backend/internal/webui/dist
	cp -r frontend/dist/. backend/internal/webui/dist/

clean:
	rm -rf frontend/dist backend/internal/webui/dist backend/nook

collab-e2e:
	scripts/collab-e2e.sh
