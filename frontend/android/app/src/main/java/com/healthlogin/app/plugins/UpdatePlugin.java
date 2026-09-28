package com.healthlogin.app.plugins;

import android.app.Activity;
import android.app.DownloadManager;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.Settings;
import android.util.Log;

import androidx.core.content.FileProvider;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.io.File;

import ru.rustore.sdk.appupdate.listener.InstallStateUpdateListener;
import ru.rustore.sdk.appupdate.manager.RuStoreAppUpdateManager;
import ru.rustore.sdk.appupdate.manager.factory.RuStoreAppUpdateManagerFactory;
import ru.rustore.sdk.appupdate.model.AppUpdateOptions;
import ru.rustore.sdk.appupdate.model.AppUpdateType;
import ru.rustore.sdk.appupdate.model.InstallStatus;
import ru.rustore.sdk.appupdate.model.UpdateAvailability;

@CapacitorPlugin(name = "AppUpdate")
public class UpdatePlugin extends Plugin {

    private static final String TAG = "AppUpdate";
    private static final String UPDATE_FILE_NAME = "healthlogin-update.apk";
    private static final String UPDATE_SUBDIR = Environment.DIRECTORY_DOWNLOADS;
    private static final int REQUEST_INSTALL_PERMISSION = 9001;
    // ActivityResult.ACTIVITY_NOT_FOUND из RuStore SDK: RuStore не установлен или устарел
    private static final int RUSTORE_ACTIVITY_NOT_FOUND = 2;

    private RuStoreAppUpdateManager ruStoreManager;
    private InstallStateUpdateListener ruStoreListener;

    private long currentDownloadId = -1;
    private BroadcastReceiver downloadReceiver;
    private PluginCall pendingInstallCall;
    private android.os.Handler progressHandler;
    private Runnable progressRunnable;

    private void startProgressTracking(DownloadManager dm, long downloadId) {
        stopProgressTracking();
        progressHandler = new android.os.Handler(android.os.Looper.getMainLooper());
        progressRunnable = new Runnable() {
            @Override
            public void run() {
                if (currentDownloadId == -1) return;
                DownloadManager.Query query = new DownloadManager.Query();
                query.setFilterById(downloadId);
                Cursor cursor = null;
                try {
                    cursor = dm.query(query);
                    if (cursor != null && cursor.moveToFirst()) {
                        int downloadedIdx = cursor.getColumnIndex(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR);
                        int totalIdx = cursor.getColumnIndex(DownloadManager.COLUMN_TOTAL_SIZE_BYTES);
                        if (downloadedIdx >= 0 && totalIdx >= 0) {
                            long downloaded = cursor.getLong(downloadedIdx);
                            long total = cursor.getLong(totalIdx);
                            int progress = total > 0 ? (int) ((downloaded * 100L) / total) : 0;
                            
                            JSObject data = new JSObject();
                            data.put("progress", progress);
                            data.put("bytesDownloaded", downloaded);
                            data.put("totalBytes", total);
                            notifyListeners("downloadProgress", data);
                        }
                    }
                } catch (Exception e) {
                    Log.w(TAG, "Error querying download progress", e);
                } finally {
                    if (cursor != null) cursor.close();
                }
                if (currentDownloadId != -1) {
                    progressHandler.postDelayed(this, 300);
                }
            }
        };
        progressHandler.post(progressRunnable);
    }

    private void stopProgressTracking() {
        if (progressHandler != null && progressRunnable != null) {
            progressHandler.removeCallbacks(progressRunnable);
        }
        progressHandler = null;
        progressRunnable = null;
    }

    @PluginMethod
    public void getCurrentVersion(PluginCall call) {
        try {
            PackageInfo info = getContext().getPackageManager().getPackageInfo(getContext().getPackageName(), 0);
            JSObject result = new JSObject();
            result.put("versionName", info.versionName);
            result.put("versionCode", Build.VERSION.SDK_INT >= Build.VERSION_CODES.P
                    ? info.getLongVersionCode()
                    : info.versionCode);
            call.resolve(result);
        } catch (PackageManager.NameNotFoundException e) {
            Log.e(TAG, "Unable to determine app version", e);
            call.reject("Unable to determine app version", e);
        }
    }

    @PluginMethod
    public void downloadAndInstall(PluginCall call) {
        String url = call.getString("url");
        if (url == null || url.isEmpty()) {
            call.reject("url is required");
            return;
        }
        call.setKeepAlive(true);

        Activity activity = getActivity();
        if (activity == null || activity.isFinishing()) {
            call.reject("Activity is not available");
            return;
        }

        // REQUEST_INSTALL_PACKAGES есть только в debug-сборке — release обновляется через RuStore
        if (!isInstallPermissionDeclared(activity)) {
            startRuStoreUpdate(call, activity, url, Boolean.TRUE.equals(call.getBoolean("force", false)));
            return;
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                && !activity.getPackageManager().canRequestPackageInstalls()) {
            pendingInstallCall = call;
            openInstallPermissionSettings(activity);
            return;
        }

        startDownload(call, url);
    }

    /**
     * Откуда ставится обновление: apk — скачиванием с нашего сервера (debug),
     * rustore — через RuStore (available — есть ли там новая версия),
     * browser — RuStore недоступен, APK открывается в браузере.
     */
    @PluginMethod
    public void checkStoreUpdate(PluginCall call) {
        Activity activity = getActivity();
        if (activity == null || activity.isFinishing()) {
            call.reject("Activity is not available");
            return;
        }

        JSObject result = new JSObject();
        if (isInstallPermissionDeclared(activity)) {
            result.put("source", "apk");
            call.resolve(result);
            return;
        }

        getRuStoreManager().getAppUpdateInfo()
                .addOnSuccessListener(info -> {
                    result.put("source", "rustore");
                    result.put("available", info.getUpdateAvailability() == UpdateAvailability.UPDATE_AVAILABLE);
                    call.resolve(result);
                })
                .addOnFailureListener(throwable -> {
                    Log.w(TAG, "RuStore update info is not available", throwable);
                    result.put("source", "browser");
                    call.resolve(result);
                });
    }

    private RuStoreAppUpdateManager getRuStoreManager() {
        if (ruStoreManager == null) {
            ruStoreManager = RuStoreAppUpdateManagerFactory.INSTANCE.create(getContext());
        }
        return ruStoreManager;
    }

    // AppUpdateInfo одноразовый, поэтому запрашивается заново перед каждым startUpdateFlow
    private void startRuStoreUpdate(PluginCall call, Activity activity, String url, boolean force) {
        RuStoreAppUpdateManager manager = getRuStoreManager();
        manager.getAppUpdateInfo()
                .addOnSuccessListener(info -> {
                    if (info.getUpdateAvailability() != UpdateAvailability.UPDATE_AVAILABLE) {
                        call.setKeepAlive(false);
                        call.reject("Update is not available in RuStore yet");
                        return;
                    }

                    int type = force && info.isUpdateTypeAllowed(AppUpdateType.IMMEDIATE)
                            ? AppUpdateType.IMMEDIATE
                            : AppUpdateType.FLEXIBLE;
                    if (type == AppUpdateType.FLEXIBLE) {
                        registerRuStoreListener(manager, call);
                    }

                    manager.startUpdateFlow(info, new AppUpdateOptions.Builder().appUpdateType(type).build())
                            .addOnSuccessListener(resultCode -> {
                                if (resultCode == RUSTORE_ACTIVITY_NOT_FOUND) {
                                    unregisterRuStoreListener();
                                    call.setKeepAlive(false);
                                    openInBrowser(call, activity, url);
                                } else if (resultCode == Activity.RESULT_CANCELED) {
                                    unregisterRuStoreListener();
                                    call.setKeepAlive(false);
                                    JSObject result = new JSObject();
                                    result.put("cancelled", true);
                                    call.resolve(result);
                                } else if (type == AppUpdateType.IMMEDIATE) {
                                    // Экран обновления и установку ведёт сам RuStore
                                    call.setKeepAlive(false);
                                    call.resolve();
                                }
                                // FLEXIBLE: вызов завершит слушатель, когда загрузка закончится
                            })
                            .addOnFailureListener(throwable -> {
                                Log.e(TAG, "RuStore update flow failed", throwable);
                                unregisterRuStoreListener();
                                call.setKeepAlive(false);
                                call.reject("RuStore update failed: " + throwable.getMessage());
                            });
                })
                .addOnFailureListener(throwable -> {
                    // RuStore не установлен, устарел или пользователь не авторизован
                    Log.w(TAG, "RuStore is not available, falling back to browser", throwable);
                    call.setKeepAlive(false);
                    openInBrowser(call, activity, url);
                });
    }

    private void registerRuStoreListener(RuStoreAppUpdateManager manager, PluginCall call) {
        unregisterRuStoreListener();
        ruStoreListener = state -> {
            switch (state.getInstallStatus()) {
                case InstallStatus.DOWNLOADING: {
                    long downloaded = state.getBytesDownloaded();
                    long total = state.getTotalBytesToDownload();
                    JSObject data = new JSObject();
                    data.put("progress", total > 0 ? (int) ((downloaded * 100L) / total) : 0);
                    data.put("bytesDownloaded", downloaded);
                    data.put("totalBytes", total);
                    notifyListeners("downloadProgress", data);
                    break;
                }
                case InstallStatus.DOWNLOADED:
                    unregisterRuStoreListener();
                    manager.completeUpdate(new AppUpdateOptions.Builder().appUpdateType(AppUpdateType.FLEXIBLE).build())
                            .addOnFailureListener(throwable -> Log.e(TAG, "RuStore install failed", throwable));
                    call.setKeepAlive(false);
                    call.resolve();
                    break;
                case InstallStatus.FAILED:
                    unregisterRuStoreListener();
                    call.setKeepAlive(false);
                    call.reject("RuStore download failed (code=" + state.getInstallErrorCode() + ")");
                    break;
                case InstallStatus.DOWNLOAD_INTERRUPTED: {
                    unregisterRuStoreListener();
                    call.setKeepAlive(false);
                    JSObject result = new JSObject();
                    result.put("cancelled", true);
                    call.resolve(result);
                    break;
                }
                default:
                    break;
            }
        };
        manager.registerListener(ruStoreListener);
    }

    private void unregisterRuStoreListener() {
        if (ruStoreListener != null && ruStoreManager != null) {
            ruStoreManager.unregisterListener(ruStoreListener);
        }
        ruStoreListener = null;
    }

    private boolean isInstallPermissionDeclared(Activity activity) {
        try {
            PackageInfo info = activity.getPackageManager().getPackageInfo(
                    activity.getPackageName(), PackageManager.GET_PERMISSIONS);
            if (info.requestedPermissions == null) {
                return false;
            }
            for (String permission : info.requestedPermissions) {
                if ("android.permission.REQUEST_INSTALL_PACKAGES".equals(permission)) {
                    return true;
                }
            }
        } catch (PackageManager.NameNotFoundException e) {
            Log.e(TAG, "Unable to read declared permissions", e);
        }
        return false;
    }

    private void openInBrowser(PluginCall call, Activity activity, String url) {
        Intent intent = new Intent(Intent.ACTION_VIEW, Uri.parse(url));
        try {
            activity.startActivity(intent);
            call.resolve();
        } catch (android.content.ActivityNotFoundException e) {
            call.reject("No application found to open update url");
        }
    }

    private void openInstallPermissionSettings(Activity activity) {
        Intent intent = new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                Uri.parse("package:" + activity.getPackageName()));
        startActivityForResult(pendingInstallCall, intent, REQUEST_INSTALL_PERMISSION);
    }

    @Override
    protected void handleOnActivityResult(int requestCode, int resultCode, Intent data) {
        super.handleOnActivityResult(requestCode, resultCode, data);
        if (requestCode != REQUEST_INSTALL_PERMISSION) {
            return;
        }

        PluginCall call = pendingInstallCall;
        pendingInstallCall = null;
        if (call == null) {
            return;
        }

        Activity activity = getActivity();
        if (activity == null || activity.isFinishing()) {
            call.reject("Activity is not available after permission request");
            return;
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                && !activity.getPackageManager().canRequestPackageInstalls()) {
            call.reject("Install permission was not granted");
            return;
        }

        String url = call.getString("url");
        startDownload(call, url);
    }

    @Override
    protected void handleOnDestroy() {
        super.handleOnDestroy();
        unregisterDownloadReceiver();
        unregisterRuStoreListener();
    }

    private void startDownload(PluginCall call, String url) {
        Context context = getContext();
        if (context == null) {
            call.reject("Context is not available");
            return;
        }

        File targetFile = getUpdateFile(context);
        File parentDir = targetFile.getParentFile();
        if (parentDir != null && !parentDir.exists() && !parentDir.mkdirs()) {
            call.reject("Unable to create update directory");
            return;
        }
        if (targetFile.exists() && !targetFile.delete()) {
            Log.w(TAG, "Unable to delete existing update file");
        }

        DownloadManager.Request request;
        try {
            request = new DownloadManager.Request(Uri.parse(url));
            request.setMimeType("application/vnd.android.package-archive");
            request.setTitle("Моя Услуга");
            request.setDescription("Загрузка обновления...");
            request.setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED);
            request.setDestinationInExternalFilesDir(context, UPDATE_SUBDIR, UPDATE_FILE_NAME);
            request.setAllowedOverMetered(true);
            request.setAllowedOverRoaming(true);
        } catch (Exception e) {
            Log.e(TAG, "Failed to create download request", e);
            call.reject("Failed to create download request: " + e.getMessage());
            return;
        }

        DownloadManager dm = (DownloadManager) context.getSystemService(Context.DOWNLOAD_SERVICE);
        if (dm == null) {
            call.reject("DownloadManager is not available");
            return;
        }

        registerDownloadReceiver(context, dm, call);

        try {
            currentDownloadId = dm.enqueue(request);
            Log.d(TAG, "Download enqueued, id=" + currentDownloadId);
            startProgressTracking(dm, currentDownloadId);
        } catch (Exception e) {
            Log.e(TAG, "Failed to enqueue download", e);
            unregisterDownloadReceiver();
            call.reject("Failed to enqueue download: " + e.getMessage());
        }
    }

    private void registerDownloadReceiver(Context context, DownloadManager dm, PluginCall call) {
        downloadReceiver = new BroadcastReceiver() {
            @Override
            public void onReceive(Context ctx, Intent intent) {
                long id = intent.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID, -1);
                if (id != currentDownloadId) {
                    return;
                }
                Log.d(TAG, "Download complete, id=" + id);
                unregisterDownloadReceiver();
                onDownloadComplete(dm, id, call);
            }
        };

        IntentFilter filter = new IntentFilter(DownloadManager.ACTION_DOWNLOAD_COMPLETE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            context.registerReceiver(downloadReceiver, filter, Context.RECEIVER_NOT_EXPORTED);
        } else {
            context.registerReceiver(downloadReceiver, filter);
        }
    }

    private void unregisterDownloadReceiver() {
        stopProgressTracking();
        if (downloadReceiver == null) {
            return;
        }
        try {
            getContext().unregisterReceiver(downloadReceiver);
        } catch (IllegalArgumentException e) {
            Log.d(TAG, "Receiver already unregistered");
        }
        downloadReceiver = null;
        currentDownloadId = -1;
    }

    private void onDownloadComplete(DownloadManager dm, long downloadId, PluginCall call) {
        DownloadManager.Query query = new DownloadManager.Query();
        query.setFilterById(downloadId);
        Cursor cursor = null;
        try {
            cursor = dm.query(query);
            if (cursor == null || !cursor.moveToFirst()) {
                call.reject("Unable to query download status");
                return;
            }

            int statusIdx = cursor.getColumnIndex(DownloadManager.COLUMN_STATUS);
            int reasonIdx = cursor.getColumnIndex(DownloadManager.COLUMN_REASON);

            int status = cursor.getInt(statusIdx);
            if (status == DownloadManager.STATUS_SUCCESSFUL) {
                installApk(call);
            } else {
                int reason = cursor.getInt(reasonIdx);
                Log.e(TAG, "Download failed, status=" + status + " reason=" + reason);
                call.reject("Download failed (status=" + status + ", reason=" + reason + ")");
            }
        } finally {
            if (cursor != null) {
                cursor.close();
            }
        }
    }

    private void installApk(PluginCall call) {
        Context context = getContext();
        Activity activity = getActivity();
        if (context == null || activity == null || activity.isFinishing()) {
            call.reject("Activity is not available for install");
            return;
        }

        File apk = getUpdateFile(context);
        Log.d(TAG, "Installing APK: " + apk.getAbsolutePath() + ", exists=" + apk.exists() + ", size=" + apk.length());

        if (!apk.exists()) {
            call.reject("Downloaded APK file not found");
            return;
        }

        Uri apkUri;
        try {
            apkUri = FileProvider.getUriForFile(context,
                    context.getPackageName() + ".fileprovider", apk);
        } catch (IllegalArgumentException e) {
            Log.e(TAG, "Failed to get content URI for APK", e);
            call.reject("Failed to get content URI for APK: " + e.getMessage());
            return;
        }

        Intent intent = new Intent(Intent.ACTION_VIEW);
        intent.setDataAndType(apkUri, "application/vnd.android.package-archive");
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        intent.addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP);

        if (intent.resolveActivity(context.getPackageManager()) == null) {
            call.reject("No application installer found on device");
            return;
        }

        activity.startActivity(intent);
        call.resolve();
    }

    private File getUpdateFile(Context context) {
        File externalDir = context.getExternalFilesDir(UPDATE_SUBDIR);
        if (externalDir != null) {
            return new File(externalDir, UPDATE_FILE_NAME);
        }
        return new File(context.getFilesDir(), UPDATE_FILE_NAME);
    }
}
