.PHONY: setup-android build-android clean start start-debug stop logs migrate migrate-docker

ANDROID_SDK_PATH ?= $(if $(wildcard $(HOME)/Android/Sdk),$(HOME)/Android/Sdk,$(HOME)/Library/Android/sdk)
JAVA_HOME ?= $(if $(wildcard /usr/lib/jvm/java-21-openjdk-amd64/bin/javac),/usr/lib/jvm/java-21-openjdk-amd64,$(if $(wildcard /usr/lib/jvm/java-17-openjdk-amd64/bin/javac),/usr/lib/jvm/java-17-openjdk-amd64,))

setup-android:
	@echo "Setting up Capacitor and Android platform..."
	cd frontend && npm install @capacitor/core @capacitor/cli @capacitor/android
	cd frontend && npx cap init healthlogin com.healthlogin.app --web-dir=dist || true
	cd frontend && npx cap add android || true
	@echo "Enabling cleartext traffic in AndroidManifest.xml..."
	node -e "const fs = require('fs'); const file = 'frontend/android/app/src/main/AndroidManifest.xml'; if (fs.existsSync(file)) { let content = fs.readFileSync(file, 'utf8'); if (!content.includes('usesCleartextTraffic')) { content = content.replace('<application', '<application android:usesCleartextTraffic=\"true\"'); fs.writeFileSync(file, content); } }"

build-android:
	@echo "Building frontend for Android..."
	cd frontend && npm run build -- --mode android
	@echo "Syncing assets to Android project..."
	cd frontend && npx cap sync
	@echo "Building APK..."
	cd frontend/android && ANDROID_HOME=$(ANDROID_SDK_PATH) ./gradlew assembleDebug
	@echo "Copying APK to project root..."
	cp frontend/android/app/build/outputs/apk/debug/app-debug.apk ./healthlogin-app.apk
	@echo "APK built successfully! You can find it at: ./healthlogin-app.apk"

# Start backend, frontend and database via Docker Compose
start:
	@echo "Starting backend, frontend and database..."
	$(call compose,up -d --build)
	@echo "Services started."
	@echo "  Backend:  https://localhost:8088"
	@echo "  Frontend: https://localhost:8443"

# Start backend with Delve remote debugger, frontend and database via Docker Compose
start-debug:
	@echo "Starting backend with Delve debugger, frontend and database..."
	$(call compose,-f docker-compose.debug.yml up -d --build)
	@echo "Debug services started."
	@echo "  Backend:  https://localhost:8088"
	@echo "  Frontend: https://localhost:8443"
	@echo "  Delve:    localhost:40000"

# Stop all running services
stop:
	@echo "Stopping all services..."
	$(call compose,down)
	@echo "Services stopped."

# Show logs from all services
logs:
	$(call compose,logs -f)

# Run database migrations manually
# Uses psql if available locally, otherwise falls back to docker exec
migrate:
	@echo "Running database migrations..."
	@if command -v psql >/dev/null 2>&1; then \
		for f in backend/migrations/*.sql; do \
			echo "Applying $$f..."; \
			psql "postgres://healthlogin:healthlogin@localhost:5432/healthlogin" -f "$$f"; \
		done; \
	else \
		echo "psql not found, applying migrations via docker exec..."; \
		for f in backend/migrations/*.sql; do \
			echo "Applying $$f..."; \
			docker exec -i healthlogin_db psql -U healthlogin -d healthlogin < "$$f"; \
		done; \
	fi
	@echo "Migrations applied."

clean:
	rm -f healthlogin-app.apk

# Helper: use docker compose if available, fall back to docker-compose
define compose
$(if $(shell docker compose version >/dev/null 2>&1 && echo ok),docker compose $(1),docker-compose $(1))
endef
