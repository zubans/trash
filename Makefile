.PHONY: setup-android build-android clean

ANDROID_SDK_PATH ?= $(HOME)/Library/Android/sdk

setup-android:
	@echo "Setting up Capacitor and Android platform..."
	cd frontend && npm install @capacitor/core @capacitor/cli @capacitor/android
	cd frontend && npx cap init healthlogin com.healthlogin.app --web-dir=dist || true
	cd frontend && npx cap add android || true
	@echo "Enabling cleartext traffic in AndroidManifest.xml..."
	node -e "const fs = require('fs'); const file = 'frontend/android/app/src/main/AndroidManifest.xml'; if (fs.existsSync(file)) { let content = fs.readFileSync(file, 'utf8'); if (!content.includes('usesCleartextTraffic')) { content = content.replace('<application', '<application android:usesCleartextTraffic=\"true\"'); fs.writeFileSync(file, content); } }"

build-android:
	@echo "Building frontend for Android..."
	cd frontend && VITE_API_URL=http://10.0.2.2:8080 npm run build
	@echo "Syncing assets to Android project..."
	cd frontend && npx cap sync
	@echo "Building APK..."
	cd frontend/android && ANDROID_HOME=$(ANDROID_SDK_PATH) ./gradlew assembleDebug
	@echo "Copying APK to project root..."
	cp frontend/android/app/build/outputs/apk/debug/app-debug.apk ./healthlogin-app.apk
	@echo "APK built successfully! You can find it at: ./healthlogin-app.apk"
