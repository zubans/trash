package com.healthlogin.app.net;

import android.content.Context;
import android.content.SharedPreferences;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.concurrent.TimeUnit;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;

/**
 * Fetches and persists the endpoint list ({@code {version, configs:[...]}}).
 *
 * <p>The list is served encrypted and behind an app key: {@link #refresh()} sends
 * {@code X-App-Key} and AES-256-GCM-decrypts the response before caching it. Only
 * the decrypted JSON lands in app-private SharedPreferences.
 *
 * <p>The list is entirely server-driven — nothing is baked into the APK. While the
 * direct path works the app refreshes and caches it every cycle, so a working list
 * is already on hand by the time direct fails. The list lives on the same backend
 * that may be blocked, so refresh() uses the process-default ProxySelector: DIRECT
 * goes direct, PROXY rides the tunnel. On failure the cached copy keeps us going.
 */
final class RemoteConfigRepo {
    private static final String TAG = "VlessConfigRepo";
    private static final String PREFS = "vless_prefs";
    private static final String KEY_BUNDLE = "bundle_json";
    private static final String KEY_SCHEMA = "schema_version";
    private static final String HDR_APP_KEY = "X-App-Key";
    // Bump when the cache contract changes. SCHEMA 2 drops the list that older
    // builds seeded from a bundled asset, so no install carries a baked-in list.
    private static final int SCHEMA = 2;

    private final SharedPreferences prefs;
    private final OkHttpClient http;
    private final String configUrl;
    private final byte[] encKey;

    RemoteConfigRepo(Context ctx, String configUrl) {
        this.prefs = ctx.getApplicationContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        this.configUrl = configUrl;
        this.encKey = AesGcm.keyFromHex(Secrets.ENC_KEY_HEX);
        // No explicit proxy: follows the JVM-default ProxySelector / channel state.
        this.http = new OkHttpClient.Builder()
                .callTimeout(10, TimeUnit.SECONDS)
                .build();

        // One-time purge: an install upgraded from a build that seeded a bundled
        // list still holds it here. Drop it so the list is only ever the server's.
        if (prefs.getInt(KEY_SCHEMA, 1) < SCHEMA) {
            prefs.edit().remove(KEY_BUNDLE).putInt(KEY_SCHEMA, SCHEMA).apply();
            DebugLog.add(TAG, "cleared stale cached endpoint list (seed removed); list is now server-only");
        }
    }

    /** Stored bundle version, or -1 if nothing is cached. */
    int version() {
        String s = prefs.getString(KEY_BUNDLE, null);
        if (s == null) return -1;
        try {
            return new JSONObject(s).optInt("version", -1);
        } catch (Throwable t) {
            return -1;
        }
    }

    /** Full configs in priority order (may be empty). */
    JSONArray configs() {
        String s = prefs.getString(KEY_BUNDLE, null);
        if (s == null) return new JSONArray();
        try {
            JSONArray a = new JSONObject(s).optJSONArray("configs");
            return a == null ? new JSONArray() : a;
        } catch (Throwable t) {
            return new JSONArray();
        }
    }

    /** @return true if the stored list changed (address update). */
    boolean refresh() {
        long t0 = System.currentTimeMillis();
        try {
            Request req = new Request.Builder()
                    .url(configUrl)
                    .header(HDR_APP_KEY, Secrets.APP_KEY)
                    .header("Cache-Control", "no-cache")
                    .build();
            try (Response resp = http.newCall(req).execute()) {
                long ms = System.currentTimeMillis() - t0;
                String cipher = resp.body() == null ? "" : resp.body().string();
                DebugLog.add(TAG, "GET " + configUrl + " -> HTTP " + resp.code()
                        + " (" + cipher.length() + "B, " + ms + "ms)");
                if (!resp.isSuccessful() || cipher.isEmpty()) {
                    return false;
                }
                String json = AesGcm.decrypt(encKey, cipher);
                JSONObject fresh = new JSONObject(json); // validate
                JSONArray freshConfigs = fresh.optJSONArray("configs");
                // An empty list is never an upgrade: it would clobber the bundled
                // seed (or a previously good list) and strand the app with no
                // servers to fall back to. Keep what we have and log it loudly.
                if (freshConfigs == null || freshConfigs.length() == 0) {
                    DebugLog.add(TAG, "server returned 0 configs — keeping cached ("
                            + configs().length() + " server(s)); populate vless-endpoints.json on the server");
                    return false;
                }
                String old = prefs.getString(KEY_BUNDLE, null);
                if (old == null || !sameBundle(new JSONObject(old), fresh)) {
                    prefs.edit().putString(KEY_BUNDLE, fresh.toString()).apply();
                    DebugLog.add(TAG, "endpoint list updated to v" + fresh.optInt("version", -1));
                    return true;
                }
            }
        } catch (Throwable t) {
            DebugLog.add(TAG, "refresh failed (keeping cached): "
                    + t.getClass().getSimpleName() + ": " + t.getMessage());
        }
        return false;
    }

    /** A secret-free description of what is cached, for the debug console. */
    String summary() {
        JSONArray configs = configs();
        StringBuilder sb = new StringBuilder("v").append(version())
                .append(", ").append(configs.length()).append(" server(s): ");
        for (int i = 0; i < configs.length(); i++) {
            JSONObject cfg = configs.optJSONObject(i);
            if (cfg == null) continue;
            if (i > 0) sb.append("; ");
            sb.append(XrayController.summarizeOutbound(cfg));
        }
        return sb.toString();
    }

    private static boolean sameBundle(JSONObject a, JSONObject b) {
        return a.optInt("version", -1) == b.optInt("version", -2)
                && String.valueOf(a.optJSONArray("configs")).equals(String.valueOf(b.optJSONArray("configs")));
    }
}
