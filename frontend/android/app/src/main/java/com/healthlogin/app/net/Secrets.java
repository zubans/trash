package com.healthlogin.app.net;

/**
 * Secrets shared with the backend for the encrypted endpoint list.
 *
 * <p><b>These must exactly match the server env</b> {@code APP_ENDPOINTS_KEY} and
 * {@code APP_ENDPOINTS_ENC_KEY} (docker-compose / .env.deploy).
 *
 * <p>Client-side secrets are inherently extractable from an APK, so this is not
 * a hard boundary — it raises the bar against casual scraping and keeps the list
 * off the server in the clear. Treat the committed values as burned: generate a
 * fresh pair before a real release and rotate both places together.
 *   APP_KEY:     openssl rand -base64 24 | tr -d '/+=' | head -c 32
 *   ENC_KEY_HEX: openssl rand -hex 32
 */
final class Secrets {
    private Secrets() {}

    /** Sent as the X-App-Key header. Matches APP_ENDPOINTS_KEY. */
    static final String APP_KEY = "s6rNCMbJXDg8q1C3Xsz1mL3KLPQPLyG";

    /** AES-256 key as 64 hex chars. Matches APP_ENDPOINTS_ENC_KEY. */
    static final String ENC_KEY_HEX =
            "91eaff0c81db619e997beaa411d5180c39ca561e586ada3c7cace00bae9de7fd";
}
