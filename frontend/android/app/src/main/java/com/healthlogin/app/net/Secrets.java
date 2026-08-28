package com.healthlogin.app.net;

import com.healthlogin.app.BuildConfig;

/**
 * Secrets shared with the backend for the encrypted endpoint list.
 *
 * <p>These are <b>not</b> hardcoded: they are injected into {@code BuildConfig}
 * at build time from {@code APP_ENDPOINTS_KEY} / {@code APP_ENDPOINTS_ENC_KEY}
 * (see app/build.gradle) — the same GitHub Actions secrets that populate the
 * server's {@code .env}. One source of truth, so app and server never drift and
 * nothing sensitive is committed. A build without those env vars (e.g. a local
 * debug build) gets empty strings, and the fallback channel simply stays off.
 *
 * <p>Client-side secrets are still extractable from an APK, so this is defence in
 * depth (keeps the list off the server in the clear and off casual scrapers),
 * not an absolute boundary.
 */
final class Secrets {
    private Secrets() {}

    /** Sent as the X-App-Key header. Mirrors server APP_ENDPOINTS_KEY. */
    static final String APP_KEY = BuildConfig.APP_ENDPOINTS_KEY;

    /** AES-256 key as 64 hex chars. Mirrors server APP_ENDPOINTS_ENC_KEY. */
    static final String ENC_KEY_HEX = BuildConfig.APP_ENDPOINTS_ENC_KEY;
}
