package com.healthlogin.app.net;

import android.content.Context;
import android.content.SharedPreferences;
import android.content.res.AssetManager;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;

/**
 * Fetches and persists the endpoint list ({@code {version, configs:[...]}}).
 *
 * <p>The list is served encrypted and behind an app key: {@link #refresh()} sends
 * {@code X-App-Key} and AES-256-GCM-decrypts the response before caching it. The
 * bundled seed asset is encrypted with the same key, so no plaintext ships in the
 * APK. Only the decrypted JSON lands in app-private SharedPreferences.
 *
 * <p>The list lives on the same backend that may be blocked, so refresh() uses the
 * process-default ProxySelector: DIRECT goes direct, PROXY rides the tunnel. On
 * failure the cached/seeded copy keeps us going.
 */
final class RemoteConfigRepo {
    private static final String TAG = "VlessConfigRepo";
    private static final String PREFS = "vless_prefs";
    private static final String KEY_BUNDLE = "bundle_json";
    private static final String HDR_APP_KEY = "X-App-Key";

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
    }

    /** Seed the cache from the encrypted bundled asset if we never fetched a list. */
    void seedFromAssetsIfEmpty(Context ctx, String assetName) {
        if (prefs.contains(KEY_BUNDLE)) return;
        AssetManager am = ctx.getApplicationContext().getAssets();
        try (InputStream in = am.open(assetName)) {
            String json = AesGcm.decrypt(encKey, readAll(in));
            new JSONObject(json); // validate
            prefs.edit().putString(KEY_BUNDLE, json).apply();
            Log.i(TAG, "seeded endpoint list from asset " + assetName);
        } catch (Throwable t) {
            Log.w(TAG, "no bundled endpoint list to seed: " + t.getMessage());
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
        try {
            Request req = new Request.Builder()
                    .url(configUrl)
                    .header(HDR_APP_KEY, Secrets.APP_KEY)
                    .build();
            try (Response resp = http.newCall(req).execute()) {
                if (!resp.isSuccessful() || resp.body() == null) return false;
                String json = AesGcm.decrypt(encKey, resp.body().string());
                JSONObject fresh = new JSONObject(json); // validate
                String old = prefs.getString(KEY_BUNDLE, null);
                if (old == null || !sameBundle(new JSONObject(old), fresh)) {
                    prefs.edit().putString(KEY_BUNDLE, fresh.toString()).apply();
                    Log.i(TAG, "endpoint list updated to version " + fresh.optInt("version", -1));
                    return true;
                }
            }
        } catch (Throwable t) {
            Log.w(TAG, "config refresh failed (keeping cached): " + t.getMessage());
        }
        return false;
    }

    private static boolean sameBundle(JSONObject a, JSONObject b) {
        return a.optInt("version", -1) == b.optInt("version", -2)
                && String.valueOf(a.optJSONArray("configs")).equals(String.valueOf(b.optJSONArray("configs")));
    }

    private static String readAll(InputStream in) throws Exception {
        ByteArrayOutputStream bos = new ByteArrayOutputStream();
        byte[] buf = new byte[8192];
        int n;
        while ((n = in.read(buf)) != -1) bos.write(buf, 0, n);
        return bos.toString("UTF-8");
    }
}
