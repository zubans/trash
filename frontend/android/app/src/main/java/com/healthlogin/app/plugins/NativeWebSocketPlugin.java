package com.healthlogin.app.plugins;

import android.util.Log;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.net.URI;
import java.util.concurrent.TimeUnit;

import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import okhttp3.WebSocket;
import okhttp3.WebSocketListener;

@CapacitorPlugin(name = "NativeWebSocket")
public class NativeWebSocketPlugin extends Plugin {

    private static final String TAG = "NativeWebSocket";
    private OkHttpClient client;
    private WebSocket webSocket;

    @Override
    public void load() {
        super.load();
        // Configure OkHttpClient to accept self-signed certificates or plain WS
        OkHttpClient.Builder builder = new OkHttpClient.Builder();
        builder.readTimeout(0, TimeUnit.MILLISECONDS);
        builder.connectTimeout(10, TimeUnit.SECONDS);
        builder.pingInterval(15, TimeUnit.SECONDS); // Automatic Ping/Pong Keep-Alive

        try {
            // Trust all certificates (Self-signed TLS support)
            javax.net.ssl.TrustManager[] trustAllCerts = new javax.net.ssl.TrustManager[]{
                new javax.net.ssl.X509TrustManager() {
                    public java.security.cert.X509Certificate[] getAcceptedIssuers() {
                        return new java.security.cert.X509Certificate[]{};
                    }
                    public void checkClientTrusted(java.security.cert.X509Certificate[] chain, String authType) {}
                    public void checkServerTrusted(java.security.cert.X509Certificate[] chain, String authType) {}
                }
            };

            javax.net.ssl.SSLContext sslContext = javax.net.ssl.SSLContext.getInstance("SSL");
            sslContext.init(null, trustAllCerts, new java.security.SecureRandom());
            builder.sslSocketFactory(sslContext.getSocketFactory(), (javax.net.ssl.X509TrustManager) trustAllCerts[0]);
            builder.hostnameVerifier((hostname, session) -> true);
        } catch (Exception e) {
            Log.e(TAG, "Error setting up trust all certs", e);
        }

        client = builder.build();
    }

    @PluginMethod
    public void connect(PluginCall call) {
        String url = call.getString("url");
        if (url == null || url.isEmpty()) {
            call.reject("URL is required");
            return;
        }

        disconnectSocket();

        try {
            Request request = new Request.Builder().url(url).build();
            webSocket = client.newWebSocket(request, new WebSocketListener() {
                @Override
                public void onOpen(WebSocket ws, Response response) {
                    Log.d(TAG, "Native WebSocket Opened");
                    JSObject ret = new JSObject();
                    ret.put("status", "open");
                    notifyListeners("onOpen", ret);
                }

                @Override
                public void onMessage(WebSocket ws, String text) {
                    JSObject ret = new JSObject();
                    ret.put("data", text);
                    notifyListeners("onMessage", ret);
                }

                @Override
                public void onClosing(WebSocket ws, int code, String reason) {
                    ws.close(1000, null);
                    JSObject ret = new JSObject();
                    ret.put("code", code);
                    ret.put("reason", reason);
                    notifyListeners("onClose", ret);
                }

                @Override
                public void onFailure(WebSocket ws, Throwable t, Response response) {
                    Log.e(TAG, "Native WebSocket Failure", t);
                    JSObject ret = new JSObject();
                    ret.put("error", t.getMessage() != null ? t.getMessage() : "Connection failed");
                    notifyListeners("onError", ret);
                }
            });
            call.resolve();
        } catch (Exception e) {
            Log.e(TAG, "Failed to connect Native WebSocket", e);
            call.reject("Failed to connect: " + e.getMessage());
        }
    }

    @PluginMethod
    public void send(PluginCall call) {
        String message = call.getString("message");
        if (message == null) {
            call.reject("Message is required");
            return;
        }
        if (webSocket != null) {
            boolean sent = webSocket.send(message);
            if (sent) {
                call.resolve();
            } else {
                call.reject("Failed to send message over socket");
            }
        } else {
            call.reject("WebSocket is not connected");
        }
    }

    @PluginMethod
    public void disconnect(PluginCall call) {
        disconnectSocket();
        call.resolve();
    }

    private void disconnectSocket() {
        if (webSocket != null) {
            try {
                webSocket.close(1000, "Disconnect requested");
            } catch (Exception e) {
                Log.w(TAG, "Error closing websocket", e);
            }
            webSocket = null;
        }
    }

    @Override
    protected void handleOnDestroy() {
        super.handleOnDestroy();
        disconnectSocket();
    }
}
